import importlib.util
import pathlib
import socket
import struct
import threading
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("warp_dns", pathlib.Path(__file__).parents[1] / "warp-dns.py")
dns = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dns)

QUERY = bytes.fromhex("123401000001000000000000076578616d706c6503636f6d0000010001")
ANSWER = bytes.fromhex("123481800001000000000000076578616d706c6503636f6d0000010001")


class FakeSocket:
    def __init__(self):
        self.received = bytearray(b"\x05\x00" + b"\x05\x00\x00\x01" + b"\x7f\x00\x00\x01\x00\x00")
        self.sent = bytearray()
        self.connected = None
    def settimeout(self, value):
        self.timeout = value
    def connect(self, address):
        self.connected = address
    def sendall(self, data):
        self.sent.extend(data)
    def recv(self, size):
        data = self.received[:size]
        del self.received[:size]
        return bytes(data)
    def close(self):
        pass


class FakeTLS(FakeSocket):
    def __enter__(self): return self
    def __exit__(self, *args): self.close()


class TestUpstream(unittest.TestCase):
    def test_socks_numeric_destination_and_verified_tls_name(self):
        sock, tls = FakeSocket(), FakeTLS()
        response = mock.Mock(status=200, length=len(ANSWER))
        response.getheader.return_value = "application/dns-message"
        response.read.return_value = ANSWER
        ctx = mock.Mock()
        ctx.wrap_socket.return_value = tls
        with mock.patch.object(dns.socket, "socket", return_value=sock), \
             mock.patch.object(dns.ssl, "create_default_context", return_value=ctx), \
             mock.patch.object(dns.http.client, "HTTPResponse", return_value=response), \
             mock.patch.object(dns.socket, "getaddrinfo", side_effect=AssertionError("system DNS forbidden")):
            self.assertEqual(ANSWER, dns.resolve_doh(QUERY))
        self.assertEqual(("127.0.0.1", 1080), sock.connected)
        self.assertEqual(b"\x05\x01\x00\x05\x01\x00\x01\x01\x01\x01\x01\x01\xbb", bytes(sock.sent))
        ctx.wrap_socket.assert_called_once_with(sock, server_hostname="cloudflare-dns.com")
        self.assertIn(b"Content-Type: application/dns-message\r\n", bytes(tls.sent))
        self.assertTrue(bytes(tls.sent).endswith(QUERY))

    def test_real_loopback_socks_wire_and_http_exchange(self):
        listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)
        listener.settimeout(2)
        self.addCleanup(listener.close)
        seen, errors = {}, []
        def socks_server():
            try:
                conn, _ = listener.accept()
                with conn:
                    conn.settimeout(2)
                    seen["greeting"] = dns.read_exact(conn, 3)
                    conn.sendall(b"\x05\x00")
                    seen["connect"] = dns.read_exact(conn, 10)
                    conn.sendall(b"\x05\x00\x00\x01\x7f\x00\x00\x01\x00\x00")
                    received = bytearray()
                    while b"\r\n\r\n" not in received:
                        received.extend(conn.recv(1024))
                    headers, body = bytes(received).split(b"\r\n\r\n", 1)
                    if len(body) < len(QUERY):
                        body += dns.read_exact(conn, len(QUERY) - len(body))
                    seen["headers"], seen["body"] = headers, body
                    conn.sendall(b"HTTP/1.1 200 OK\r\nContent-Type: application/dns-message\r\nContent-Length: " + str(len(ANSWER)).encode() + b"\r\n\r\n" + ANSWER)
            except Exception as exc:
                errors.append(exc)
        thread = threading.Thread(target=socks_server)
        thread.start()
        ctx = mock.Mock()
        ctx.wrap_socket.side_effect = lambda sock, server_hostname: sock
        try:
            with mock.patch.object(dns, "SOCKS_PORT", listener.getsockname()[1]), \
                 mock.patch.object(dns.ssl, "create_default_context", return_value=ctx), \
                 mock.patch.object(dns.socket, "getaddrinfo", side_effect=AssertionError("system DNS forbidden")):
                self.assertEqual(ANSWER, dns.resolve_doh(QUERY))
        finally:
            thread.join(timeout=3)
        self.assertFalse(thread.is_alive())
        self.assertFalse(errors)
        self.assertEqual(b"\x05\x01\x00", seen["greeting"])
        self.assertEqual(b"\x05\x01\x00\x01\x01\x01\x01\x01\x01\xbb", seen["connect"])
        self.assertIn(b"Host: cloudflare-dns.com", seen["headers"])
        self.assertEqual(QUERY, seen["body"])
        self.assertEqual("cloudflare-dns.com", ctx.wrap_socket.call_args.kwargs["server_hostname"])

    def test_invalid_upstream_response_fails_closed(self):
        for result in (OSError("offline"), ValueError("invalid")):
            with self.subTest(result=result), mock.patch.object(dns, "resolve_doh", side_effect=result):
                reply = dns.answer(QUERY)
                self.assertEqual(QUERY[:2], reply[:2])
                self.assertEqual(2, reply[3] & 15)
                self.assertEqual(QUERY[12:], reply[12:])

    def test_rejects_wrong_transaction(self):
        sock, tls = FakeSocket(), FakeTLS()
        response = mock.Mock(status=200, length=len(ANSWER))
        response.getheader.return_value = "application/dns-message"
        response.read.return_value = b"\x99\x99" + ANSWER[2:]
        ctx = mock.Mock()
        ctx.wrap_socket.return_value = tls
        with mock.patch.object(dns.socket, "socket", return_value=sock), mock.patch.object(dns.ssl, "create_default_context", return_value=ctx), mock.patch.object(dns.http.client, "HTTPResponse", return_value=response):
            with self.assertRaises(ValueError): dns.resolve_doh(QUERY)

    def test_truncated_http_body_is_rejected(self):
        sock, tls = FakeSocket(), FakeTLS()
        response = mock.Mock(status=200, length=len(ANSWER) + 5)
        response.getheader.return_value = "application/dns-message"
        response.read.return_value = ANSWER
        ctx = mock.Mock()
        ctx.wrap_socket.return_value = tls
        with mock.patch.object(dns.socket, "socket", return_value=sock), \
             mock.patch.object(dns.ssl, "create_default_context", return_value=ctx), \
             mock.patch.object(dns.http.client, "HTTPResponse", return_value=response):
            self.assertEqual(2, dns.answer(QUERY)[3] & 15)

    def test_socks_rejection_never_attempts_tls(self):
        sock = FakeSocket()
        sock.received = bytearray(b"\x05\xff")
        with mock.patch.object(dns.socket, "socket", return_value=sock), \
             mock.patch.object(dns.ssl, "create_default_context") as context:
            with self.assertRaises(OSError):
                dns.resolve_doh(QUERY)
        self.assertEqual(("127.0.0.1", 1080), sock.connected)
        context.assert_not_called()

    def test_http_failure_and_oversize_fail_closed(self):
        for status, body in ((503, ANSWER), (200, ANSWER + b"x" * dns.MAX_DNS)):
            sock, tls = FakeSocket(), FakeTLS()
            response = mock.Mock(status=status, length=len(body))
            response.getheader.return_value = "application/dns-message"
            response.read.return_value = body
            ctx = mock.Mock()
            ctx.wrap_socket.return_value = tls
            with mock.patch.object(dns.socket, "socket", return_value=sock), \
                 mock.patch.object(dns.ssl, "create_default_context", return_value=ctx), \
                 mock.patch.object(dns.http.client, "HTTPResponse", return_value=response):
                self.assertEqual(2, dns.answer(QUERY)[3] & 15)


class TestServers(unittest.TestCase):
    def test_udp_and_tcp_length_framing(self):
        proxy = dns.DNSProxy("127.0.0.1", 0, max_concurrent=2)
        with mock.patch.object(dns, "resolve_doh", return_value=ANSWER):
            proxy.start()
            self.addCleanup(proxy.close)
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
                client.settimeout(2)
                client.sendto(QUERY, proxy.udp_address)
                self.assertEqual(ANSWER, client.recv(4096))
            with socket.create_connection(proxy.tcp_address, timeout=2) as client:
                client.sendall(struct.pack("!H", len(QUERY)) + QUERY)
                size = struct.unpack("!H", dns.read_exact(client, 2))[0]
                self.assertEqual(ANSWER, dns.read_exact(client, size))
                client.sendall(struct.pack("!H", len(QUERY)) + QUERY)
                size = struct.unpack("!H", dns.read_exact(client, 2))[0]
                self.assertEqual(ANSWER, dns.read_exact(client, size))

    def test_oversized_tcp_query_fails_without_upstream(self):
        proxy = dns.DNSProxy("127.0.0.1", 0, max_concurrent=1)
        with mock.patch.object(dns, "resolve_doh") as upstream:
            proxy.start()
            self.addCleanup(proxy.close)
            with socket.create_connection(proxy.tcp_address, timeout=2) as client:
                client.sendall(struct.pack("!H", dns.MAX_DNS + 1) + QUERY[:2])
                size = struct.unpack("!H", dns.read_exact(client, 2))[0]
                result = dns.read_exact(client, size)
                self.assertEqual(QUERY[:2], result[:2])
                self.assertEqual(2, result[3] & 15)
            upstream.assert_not_called()

    def test_concurrency_is_bounded_and_overload_servfails(self):
        entered, release = threading.Event(), threading.Event()
        def slow(_):
            entered.set()
            release.wait(2)
            return ANSWER
        proxy = dns.DNSProxy("127.0.0.1", 0, max_concurrent=1)
        with mock.patch.object(dns, "resolve_doh", side_effect=slow):
            proxy.start()
            self.addCleanup(proxy.close)
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as a, socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as b:
                a.settimeout(2); b.settimeout(2)
                a.sendto(QUERY, proxy.udp_address)
                self.assertTrue(entered.wait(1))
                b.sendto(QUERY, proxy.udp_address)
                self.assertEqual(2, b.recv(4096)[3] & 15)
                release.set()
                self.assertEqual(ANSWER, a.recv(4096))

    def test_tcp_overload_gets_servfail(self):
        entered, release = threading.Event(), threading.Event()
        def slow(_):
            entered.set()
            release.wait(2)
            return ANSWER
        proxy = dns.DNSProxy("127.0.0.1", 0, max_concurrent=1)
        with mock.patch.object(dns, "resolve_doh", side_effect=slow):
            proxy.start()
            self.addCleanup(proxy.close)
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as active:
                active.settimeout(2)
                active.sendto(QUERY, proxy.udp_address)
                self.assertTrue(entered.wait(1))
                with socket.create_connection(proxy.tcp_address, timeout=2) as client:
                    client.sendall(struct.pack("!H", len(QUERY)) + QUERY)
                    size = struct.unpack("!H", dns.read_exact(client, 2))[0]
                    self.assertEqual(2, dns.read_exact(client, size)[3] & 15)
                release.set()
                self.assertEqual(ANSWER, active.recv(4096))


if __name__ == "__main__": unittest.main()
