import unittest
import json
from types import SimpleNamespace
from unittest.mock import Mock, patch

from tools import openai_oauth_reauth_worker as worker


class WarpWorkerTests(unittest.TestCase):
    def setUp(self):
        self.enterContext(patch.dict('os.environ', {'OPENAI_WARP_WORKER_NETWORK_ISOLATED': 'true'}))

    def test_codex_pool_cli_executes_login_and_submits_credentials(self):
        api = Mock()
        api.credentials.return_value = {'status': 'succeeded'}
        claim = {'task_id': 7, 'account_id': 42, 'login_email': 'demo@example.test',
                 'password': 'synthetic-password', 'totp_secret': 'synthetic-totp',
                 'oauth_profile': 'codex', 'engine': 'local_worker',
                 'proxy_url': 'socks5h://warp:1080', 'warp_required': True}
        with patch.dict('os.environ', {'OPENAI_REAUTH_CODEX_POOL_URL': 'http://relogin:8787/api/v1/relogin'}), \
             patch.object(worker.NO_REDIRECT_OPENER, 'open') as open_request, \
             patch.object(worker.subprocess, 'run') as subprocess_run:
            open_request.return_value.__enter__.return_value.read.return_value = json.dumps({
                'credential': {'access_token': 'access', 'refresh_token': 'refresh', 'id_token': 'identity'},
            }).encode()
            worker.process_password_claim(api, claim)
            request = open_request.call_args.args[0]
            self.assertEqual(request.full_url, 'http://relogin:8787/api/v1/relogin')
            self.assertEqual(json.loads(request.data), {
                'email': 'demo@example.test', 'password': 'synthetic-password', 'mfa_secret': 'synthetic-totp'})
            api.credentials.assert_called_once_with(7, {
                'access_token': 'access', 'refresh_token': 'refresh', 'id_token': 'identity'}, {})
            subprocess_run.assert_not_called()

    def test_codex_pool_cli_rejects_incomplete_credentials(self):
        api = Mock()
        claim = {'task_id': 7, 'login_email': 'demo@example.test', 'password': 'synthetic-password'}
        with patch.object(worker.NO_REDIRECT_OPENER, 'open') as open_request:
            open_request.return_value.__enter__.return_value.read.return_value = b'{"credential":{"access_token":"x"}}'
            with self.assertRaisesRegex(worker.WorkerError, 'incomplete OAuth credentials'):
                worker.process_codex_pool_claim(api, claim, 'http://relogin:8787/api/v1/relogin')
            api.credentials.assert_not_called()

    def test_disabled_warp_restores_legacy_direct_or_account_proxy(self):
        with patch.dict('os.environ', {'OPENAI_WARP_WORKER_NETWORK_ISOLATED': 'false'}):
            worker.require_warp_worker_egress({'warp_required': False, 'proxy_url': ''})
            worker.require_warp_worker_egress({'warp_required': False, 'proxy_url': 'http://account-proxy:8080'})

    def test_unconfirmed_isolation_blocks_oauth_before_protocol_or_subprocess(self):
        claim = {'credential_mode': 'password_totp', 'proxy_url': 'socks5h://warp:1080'}
        with patch.dict('os.environ', {'OPENAI_WARP_WORKER_NETWORK_ISOLATED': 'false'}), \
             patch.object(worker.subprocess, 'run') as spawn:
            with self.assertRaisesRegex(worker.WorkerError, 'OPENAI_WARP_WORKER_NETWORK_ISOLATED=true'):
                worker.process_claim(Mock(), Mock(), claim)
            spawn.assert_not_called()

    def test_missing_confirmation_blocks_email_before_protocol(self):
        protocol = Mock()
        claim = {'credential_mode': 'email_otp_url', 'proxy_url': 'socks5h://warp:1080'}
        with patch.dict('os.environ', {}, clear=True):
            with self.assertRaisesRegex(worker.WorkerError, 'OPENAI_WARP_WORKER_NETWORK_ISOLATED=true'):
                worker.process_claim(Mock(), protocol, claim)
        protocol.ProtocolSession.assert_not_called()

    def test_unconfirmed_isolation_blocks_totp_and_session_export(self):
        api = Mock()
        api.claim_totp.return_value = {'task_id': 1, 'proxy_url': 'socks5h://warp:1080'}
        with patch.dict('os.environ', {'OPENAI_WARP_WORKER_NETWORK_ISOLATED': ''}), \
             patch('tools.openai_totp_rotation.rotate') as rotate:
            self.assertTrue(worker.run_once(api, None))
            rotate.assert_not_called()
            api.totp_finish.assert_called_once_with(1, False, 'login_failed')
            with self.assertRaisesRegex(worker.WorkerError, 'OPENAI_WARP_WORKER_NETWORK_ISOLATED=true'):
                worker.process_session_export_claim(api, {'proxy_url': 'socks5h://warp:1080'})

    def test_confirmed_isolation_passes_warp_to_totp(self):
        api = Mock()
        claim = {'task_id': 1, 'proxy_url': 'socks5h://warp:1080'}
        api.claim_totp.return_value = claim
        with patch('tools.openai_totp_rotation.rotate') as rotate:
            self.assertTrue(worker.run_once(api, None))
            rotate.assert_called_once_with(api, claim)

    def test_all_claims_send_explicit_confirmation(self):
        api = worker.WorkerAPI(SimpleNamespace(worker_id='worker', tosub2_root='runtime'))
        api._post = Mock(return_value=None)
        for flag, expected in [('true', True), ('false', False), ('1', False), ('', False)]:
            with self.subTest(flag=flag), patch.dict('os.environ', {
                'OPENAI_WARP_WORKER_NETWORK_ISOLATED': flag, 'OPENAI_TOTP_JOURNAL_DIR': 'test',
            }), patch('tools.openai_totp_rotation.Journal'):
                api.claim()
                api.claim_session_export()
                api.claim_totp()
                for call in api._post.call_args_list[-3:]:
                    self.assertIs(call.args[1]['warp_network_isolated'], expected)

    def test_reauth_requires_proxy_before_protocol(self):
        protocol = Mock()
        with self.assertRaisesRegex(worker.WorkerError, 'proxy_unavailable'):
            worker.process_claim(Mock(), protocol, {'credential_mode': 'email_otp_url'})
        protocol.ProtocolSession.assert_not_called()

    def test_session_studio_never_sends_credentials(self):
        with patch.object(worker.NO_REDIRECT_OPENER, 'open') as request:
            with self.assertRaisesRegex(worker.WorkerError, 'cannot guarantee WARP'):
                worker.process_password_claim(Mock(), {'engine': 'session_studio', 'proxy_url': 'socks5h://warp:1080'})
            request.assert_not_called()

    def test_rotation_missing_proxy_fails_before_login(self):
        api = Mock()
        api.claim_totp.return_value = {'task_id': 1}
        self.assertTrue(worker.run_once(api, None))
        api.totp_finish.assert_called_once_with(1, False, 'login_failed')
        api.claim.assert_not_called()

    def test_session_export_requires_proxy(self):
        with self.assertRaisesRegex(worker.WorkerError, 'proxy_unavailable'):
            worker.process_session_export_claim(Mock(), {})


if __name__ == '__main__':
    unittest.main()
