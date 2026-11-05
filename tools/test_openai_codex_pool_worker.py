import json
import sys
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from tools import openai_oauth_reauth_worker as worker


class CodexPoolWorkerTests(unittest.TestCase):
    def test_private_app_http_requires_explicit_internal_setting(self):
        with patch.dict('os.environ', {'OPENAI_REAUTH_ALLOW_PRIVATE_HTTP': 'true'}):
            self.assertTrue(worker.allow_private_worker_http('sub2api'))
            self.assertFalse(worker.allow_private_worker_http('other-service'))
        with patch.dict('os.environ', {'OPENAI_REAUTH_ALLOW_PRIVATE_HTTP': 'false'}):
            self.assertFalse(worker.allow_private_worker_http('sub2api'))

    def test_codex_pool_executes_login_and_submits_credentials(self):
        api = Mock()
        api.credentials.return_value = {'status': 'succeeded'}
        claim = {'task_id': 7, 'account_id': 42, 'login_email': 'demo@example.test',
                 'password': 'synthetic-password', 'totp_secret': 'synthetic-totp',
                 'oauth_profile': 'codex', 'engine': 'local_worker'}
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

    def test_codex_pool_rejects_incomplete_credentials(self):
        api = Mock()
        claim = {'task_id': 7, 'login_email': 'demo@example.test', 'password': 'synthetic-password'}
        with patch.object(worker.NO_REDIRECT_OPENER, 'open') as open_request:
            open_request.return_value.__enter__.return_value.read.return_value = b'{"credential":{"access_token":"x"}}'
            with self.assertRaisesRegex(worker.WorkerError, 'incomplete OAuth credentials'):
                worker.process_codex_pool_claim(api, claim, 'http://relogin:8787/api/v1/relogin')
            api.credentials.assert_not_called()

    def test_account_proxy_does_not_go_to_global_codex_pool_exit(self):
        api = Mock()
        api.config.tosub2_root = Path('/nonexistent-codex-protocol-test')
        claim = {'task_id': 7, 'account_id': 42, 'login_email': 'demo@example.test',
                 'password': 'synthetic-password', 'oauth_profile': 'codex',
                 'engine': 'local_worker', 'proxy_url': 'http://account-proxy.example:8080'}
        with patch.dict('os.environ', {'OPENAI_REAUTH_CODEX_POOL_URL': 'http://relogin:8787/api/v1/relogin',
                                      'NODE_EXECUTABLE': sys.executable}), \
             patch.object(worker, 'process_codex_pool_claim') as delegate:
            with self.assertRaisesRegex(worker.WorkerError, 'toSub2 protocol-login.mjs is unavailable'):
                worker.process_password_claim(api, claim)
            delegate.assert_not_called()


if __name__ == '__main__':
    unittest.main()
