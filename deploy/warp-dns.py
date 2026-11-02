#!/usr/bin/env python3
"""A bounded loopback DNS-to-HTTPS bridge whose only egress is local SOCKS5."""

import http.client
import socket
import ssl
import struct
import threading
import time

LISTEN_HOST = "127.0.0.1"
LISTEN_PORT = 1053
SOCKS_HOST = "127.0.0.1"
SOCKS_PORT = 1080
DOH_IP = "1.1.1.1"
DOH_HOST = "cloudflare-dns.com"
MAX_DNS = 4096
TIMEOUT = 5.0
MAX_CONCURRENT = 64


def read_exact(sock, count, deadline=None):
    deadline = time.monotonic() + TIMEOUT if deadline is None else deadline
    out = bytearray()
    while len(out) < count:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("read deadline exceeded")
        sock.settimeout(remaining)
        chunk = sock.recv(count - len(out))
        if not chunk:
            raise EOFError("unexpected EOF")
        out.extend(chunk)
    return bytes(out)


def servfail(query):
    """Return a minimal failure, echoing a single well-formed question if present."""
    ident = (query[:2] + b"\0\0")[:2]
    flags = struct.unpack("!H", query[2:4])[0] if len(query) >= 4 else 0
    question = b""
    if len(query) >= 12 and query[4:6] == b"\0\1":
        pos = 12
        while pos < len(query):
            length = query[pos]
            pos += 1
            if length == 0:
                if pos + 4 <= len(query):
                    question = query[12:pos + 4]
                break
            if length > 63 or pos + length > len(query):
                break
            pos += length
    # Preserve the query opcode, recursion-desired and checking-disabled bits.
    response_flags = 0x8000 | (flags & 0x7910) | 2
    return ident + struct.pack("!HHHHH", response_flags, bool(question), 0, 0, 0) + question


def resolve_doh(query):
    if not 12 <= len(query) <= MAX_DNS or query[2] & 0x80:
        raise ValueError("invalid DNS query")
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        sock.settimeout(TIMEOUT)
        # Both addresses passed to the kernel are numeric; no resolver is used.
        sock.connect((SOCKS_HOST, SOCKS_PORT))
        sock.sendall(b"\x05\x01\x00")
        deadline = time.monotonic() + TIMEOUT
        if read_exact(sock, 2, deadline) != b"\x05\x00":
            raise OSError("SOCKS authentication rejected")
        sock.sendall(b"\x05\x01\x00\x01" + socket.inet_aton(DOH_IP) + struct.pack("!H", 443))
        reply = read_exact(sock, 4, deadline)
        if reply[:3] != b"\x05\x00\x00":
            raise OSError("SOCKS connection rejected")
        if reply[3] == 1:
            length = 4
        elif reply[3] == 4:
            length = 16
        elif reply[3] == 3:
            length = read_exact(sock, 1, deadline)[0]
        else:
            raise OSError("invalid SOCKS address")
        read_exact(sock, length + 2, deadline)
        context = ssl.create_default_context()
        with context.wrap_socket(sock, server_hostname=DOH_HOST) as tls:
            tls.settimeout(TIMEOUT)
            def abort_slow_response():
                try:
                    tls.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass
            watchdog = threading.Timer(TIMEOUT, abort_slow_response)
            watchdog.daemon = True
            watchdog.start()
            request = (
                "POST /dns-query HTTP/1.1\r\n"
                f"Host: {DOH_HOST}\r\n"
                "Content-Type: application/dns-message\r\n"
                "Accept: application/dns-message\r\n"
                f"Content-Length: {len(query)}\r\n"
                "Connection: close\r\n\r\n"
            ).encode("ascii") + query
            response = None
            try:
                tls.sendall(request)
                response = http.client.HTTPResponse(tls)
                response.begin()
                if response.status != 200 or (response.getheader("Content-Type") or "").split(";", 1)[0].strip().lower() != "application/dns-message":
                    raise ValueError("invalid DoH response")
                expected_length = response.length
                if expected_length is not None and expected_length > MAX_DNS:
                    raise ValueError("oversized DoH response")
                body = response.read(MAX_DNS + 1)
                if (expected_length is not None and len(body) != expected_length) or len(body) < 12 or len(body) > MAX_DNS or body[:2] != query[:2] or not body[2] & 0x80:
                    raise ValueError("invalid DNS response")
                return body
            finally:
                watchdog.cancel()
                if response is not None:
                    response.close()
    finally:
        sock.close()


def answer(query):
    try:
        return resolve_doh(query)
    except Exception:
        return servfail(query)


class DNSProxy:
    def __init__(self, host=LISTEN_HOST, port=LISTEN_PORT, max_concurrent=MAX_CONCURRENT):
        if host != LISTEN_HOST or max_concurrent < 1:
            raise ValueError("DNS proxy requires loopback and positive concurrency")
        self.slots = threading.BoundedSemaphore(max_concurrent)
        self.stop = threading.Event()
        self.udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.tcp = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            self.udp.bind((host, port))
            self.tcp.bind((host, port))
            self.tcp.listen(max_concurrent)
            self.udp.settimeout(0.2)
            self.tcp.settimeout(0.2)
        except Exception:
            self.udp.close()
            self.tcp.close()
            raise
        self.udp_address = self.udp.getsockname()
        self.tcp_address = self.tcp.getsockname()
        self.threads = []

    def start(self):
        for target in (self._udp_loop, self._tcp_loop):
            thread = threading.Thread(target=target, daemon=True)
            self.threads.append(thread)
            thread.start()

    def close(self):
        self.stop.set()
        self.udp.close()
        self.tcp.close()
        for thread in self.threads:
            thread.join(timeout=1)

    def _spawn(self, target, *args):
        if not self.slots.acquire(blocking=False):
            return False
        def work():
            try:
                target(*args)
            finally:
                self.slots.release()
        try:
            threading.Thread(target=work, daemon=True).start()
        except Exception:
            self.slots.release()
            raise
        return True

    def _udp_loop(self):
        while not self.stop.is_set():
            try:
                query, address = self.udp.recvfrom(MAX_DNS + 1)
                if not self._spawn(self._udp_request, query, address):
                    self.udp.sendto(servfail(query), address)
            except socket.timeout:
                continue
            except OSError:
                if self.stop.is_set():
                    return

    def _udp_request(self, query, address):
        try:
            self.udp.sendto(answer(query), address)
        except OSError:
            pass

    def _tcp_loop(self):
        while not self.stop.is_set():
            try:
                client, _ = self.tcp.accept()
                if not self._spawn(self._tcp_request, client):
                    self._tcp_busy(client)
            except socket.timeout:
                continue
            except OSError:
                if self.stop.is_set():
                    return

    def _tcp_busy(self, client):
        # One bounded read in the accept loop avoids spawning overflow workers.
        with client:
            try:
                deadline = time.monotonic() + 0.2
                length = struct.unpack("!H", read_exact(client, 2, deadline))[0]
                query = read_exact(client, min(length, 2 if length > MAX_DNS else MAX_DNS), deadline)
                result = servfail(query)
                client.settimeout(0.2)
                client.sendall(struct.pack("!H", len(result)) + result)
            except (OSError, EOFError, ValueError, struct.error):
                pass

    def _tcp_request(self, client):
        with client:
            try:
                # Bound both connection lifetime and requests per connection.
                connection_deadline = time.monotonic() + 30.0
                for _ in range(32):
                    deadline = min(connection_deadline, time.monotonic() + TIMEOUT)
                    length = struct.unpack("!H", read_exact(client, 2, deadline))[0]
                    if length > MAX_DNS:
                        query = read_exact(client, min(length, 2), deadline)
                        result = servfail(query)
                    else:
                        query = read_exact(client, length, deadline)
                        result = answer(query)
                    client.settimeout(TIMEOUT)
                    client.sendall(struct.pack("!H", len(result)) + result)
                    if length > MAX_DNS:
                        return
            except (OSError, EOFError, ValueError, struct.error):
                pass


def main():
    proxy = DNSProxy()
    proxy.start()
    try:
        threading.Event().wait()
    except KeyboardInterrupt:
        pass
    finally:
        proxy.close()


if __name__ == "__main__":
    main()
