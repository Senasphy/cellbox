import io
import json
import runpy
from pathlib import Path
import os
import unittest
from unittest.mock import patch


def _load_example():
    path = Path(__file__).resolve().parents[3] / "examples" / "fix-loop.py"
    return runpy.run_path(str(path))


class FixLoopExampleTests(unittest.TestCase):
    def test_fake_model_returns_deterministic_repair(self):
        suggest_fix = _load_example()["suggest_fix"]
        self.assertEqual(suggest_fix("broken", "assertion failed", fake_model=True), "def add(a, b):\n    return a + b\n")


    def test_hosted_model_uses_configured_api_and_extracts_code(self):
        suggest_fix = _load_example()["suggest_fix"]
        response = io.BytesIO(json.dumps({"choices": [{"message": {"content": "```python\ndef add(a, b):\n    return a + b\n```"}}]}).encode())
        with patch.dict(os.environ, {"OPENAI_API_KEY": "test-token", "OPENAI_BASE_URL": "https://model.example/v1/"}):
            with patch("urllib.request.urlopen", return_value=response) as urlopen:
                fixed = suggest_fix("def add(a,b): return a-b", "assertion failed", fake_model=False)
        self.assertEqual(fixed, "def add(a, b):\n    return a + b")
        request = urlopen.call_args.args[0]
        self.assertEqual(request.full_url, "https://model.example/v1/chat/completions")
        self.assertEqual(request.get_header("Authorization"), "Bearer test-token")
        self.assertTrue(json.loads(request.data)["messages"][1]["content"].endswith("assertion failed"))


if __name__ == "__main__":
    unittest.main()
