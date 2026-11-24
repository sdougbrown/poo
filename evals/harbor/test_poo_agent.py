"""Unit tests for the Harbor poo adapter (no Harbor or container runtime required)."""

from __future__ import annotations

import asyncio
import json
import os
import tempfile
import unittest
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from unittest.mock import patch

from poo_agent import (
    DEFAULT_BIN_TARGET,
    PooAgent,
    parse_session_log,
    resolve_endpoint_and_model,
)


@dataclass
class FakeExecResult:
    stdout: str = ""
    stderr: str = ""
    return_code: int = 0


class FakeEnvironment:
    def __init__(self, *, log_lines: list[str] | None = None, mount_logs_dir: Path | None = None):
        self.commands: list[tuple[str, dict[str, str] | None]] = []
        self.uploads: list[tuple[Path, str]] = []
        self.downloads: list[tuple[str, Path]] = []
        self._log_lines = log_lines or []
        self._mount_logs_dir = mount_logs_dir

    async def exec(
        self,
        command: str,
        env: dict[str, str] | None = None,
        **kwargs: Any,
    ) -> FakeExecResult:
        self.commands.append((command, env))
        if self._mount_logs_dir is not None and " run " in command and self._log_lines:
            (self._mount_logs_dir / "session.jsonl").write_text(
                "\n".join(self._log_lines) + "\n", encoding="utf-8"
            )
        return FakeExecResult(stdout="final answer\n", stderr="progress\n", return_code=0)

    async def upload_file(self, source_path: Path | str, target_path: str) -> None:
        self.uploads.append((Path(source_path), target_path))

    async def download_file(self, source_path: str, target_path: Path | str) -> None:
        target = Path(target_path)
        self.downloads.append((source_path, target))
        if self._log_lines:
            target.write_text("\n".join(self._log_lines) + "\n", encoding="utf-8")


@dataclass
class FakeContext:
    n_input_tokens: int | None = None
    n_output_tokens: int | None = None
    n_cache_tokens: int | None = None
    cost_usd: float | None = None
    metadata: dict[str, Any] = field(default_factory=dict)


class ParseSessionLogTests(unittest.TestCase):
    def test_parses_usage_tools_compaction_and_settle(self) -> None:
        records = [
            {"record": "session.created", "context_limit": 131072},
            {"record": "usage", "input_tokens": 1000, "output_tokens": 200, "cache_read_tokens": 400},
            {"record": "tool.call", "tool": "bash"},
            {"record": "tool.result", "outcome": "ok"},
            {"record": "tool.call", "tool": "edit"},
            {"record": "tool.result", "outcome": "error"},
            {"record": "compaction", "strategy": "summary"},
            {"record": "usage", "input_tokens": 600, "output_tokens": 150, "cache_read_tokens": 100},
            {"record": "run.settled", "reason": "finished", "turns": 3, "detail": {"source": "model"}},
        ]
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "session.jsonl"
            log_path.write_text("\n".join(json.dumps(r) for r in records) + "\n", encoding="utf-8")

            summary = parse_session_log(log_path)
            self.assertEqual(summary.input_tokens, 1600)
            self.assertEqual(summary.output_tokens, 350)
            self.assertEqual(summary.cache_read_tokens, 500)
            self.assertEqual(summary.turns, 3)
            self.assertEqual(summary.tool_calls, 2)
            self.assertEqual(summary.tool_errors, 1)
            self.assertEqual(summary.compactions, 1)
            self.assertEqual(summary.settle_reason, "finished")
            self.assertEqual(summary.settle_source, "model")
            self.assertFalse(summary.torn)

    def test_tolerates_torn_last_line(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "session.jsonl"
            log_path.write_text(
                json.dumps({"record": "usage", "input_tokens": 500, "output_tokens": 50})
                + "\n"
                + '{"record": "usage", "input_tok',
                encoding="utf-8",
            )
            summary = parse_session_log(log_path)
            self.assertEqual(summary.input_tokens, 500)
            self.assertEqual(summary.output_tokens, 50)
            self.assertIsNone(summary.cache_read_tokens)
            self.assertTrue(summary.torn)


class ProviderResolutionTests(unittest.TestCase):
    def test_known_provider_prefix(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            ep, mid, key = resolve_endpoint_and_model("openai/gpt-4o", None, None, None)
            self.assertEqual(ep, "https://api.openai.com/v1")
            self.assertEqual(mid, "gpt-4o")
            self.assertEqual(key, "OPENAI_API_KEY")

    def test_explicit_endpoint_overrides_provider(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            ep, mid, key = resolve_endpoint_and_model(
                "openai/gpt-4o",
                "http://grace:4000/v1",
                "qwen",
                "HOME_KEY",
            )
            self.assertEqual(ep, "http://grace:4000/v1")
            self.assertEqual(mid, "qwen")
            self.assertEqual(key, "HOME_KEY")


class PooAgentLifecycleTests(unittest.TestCase):
    def test_setup_and_run_with_mounted_logs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            fake_bin = tmp_path / "poo-linux-amd64"
            fake_bin.write_bytes(b"#!/bin/sh\necho dev\n")
            logs_dir = tmp_path / "logs"

            agent = PooAgent(
                logs_dir=logs_dir,
                model_name="openai/gpt-4o",
                poo_bin=str(fake_bin),
                context_limit=65536,
                max_turns=40,
                tools_mode="native",
                reasoning_effort="medium",
            )

            log_lines = [
                json.dumps({"record": "usage", "input_tokens": 2000, "output_tokens": 400, "cache_read_tokens": 800}),
                json.dumps({"record": "tool.call", "tool": "bash"}),
                json.dumps({"record": "tool.result", "outcome": "ok"}),
                json.dumps({"record": "run.settled", "reason": "finished", "turns": 2}),
            ]
            env = FakeEnvironment(log_lines=log_lines, mount_logs_dir=logs_dir)
            ctx = FakeContext()

            with patch.dict(os.environ, {"OPENAI_API_KEY": "sk-test"}, clear=False):
                asyncio.run(agent.setup(env))
                asyncio.run(agent.run("Fix the bug in `foo.py`\nwith 'quotes' & $vars.", env, ctx))

            # Binary was uploaded and marked executable.
            self.assertIn((fake_bin, DEFAULT_BIN_TARGET), env.uploads)
            self.assertTrue(any("chmod +x" in cmd for cmd, _ in env.commands))

            # Run command passed API key via env, not CLI args.
            run_cmd, run_env = env.commands[-1]
            self.assertIn("--endpoint https://api.openai.com/v1", run_cmd)
            self.assertIn("--model-id gpt-4o", run_cmd)
            self.assertIn("--context-limit 65536", run_cmd)
            self.assertIn("--max-turns 40", run_cmd)
            self.assertIn("--tools-mode native", run_cmd)
            self.assertIn("--reasoning-effort medium", run_cmd)
            self.assertIn("--permission-unmatched allow", run_cmd)
            self.assertIn("--permission-fallback allow", run_cmd)
            self.assertNotIn("sk-test", run_cmd)
            self.assertEqual(run_env, {"OPENAI_API_KEY": "sk-test"})

            # Context populated from session.jsonl.
            self.assertEqual(ctx.n_input_tokens, 2000)
            self.assertEqual(ctx.n_output_tokens, 400)
            self.assertEqual(ctx.n_cache_tokens, 800)
            self.assertEqual(ctx.metadata["turns"], 2)
            self.assertEqual(ctx.metadata["tool_calls"], 1)
            self.assertEqual(ctx.metadata["settle_reason"], "finished")

    def test_run_downloads_log_when_not_mounted(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            fake_bin = tmp_path / "poo-linux-amd64"
            fake_bin.write_bytes(b"binary")
            settings = tmp_path / "settings.yaml"
            settings.write_text("version: 1\n", encoding="utf-8")
            logs_dir = tmp_path / "logs"

            agent = PooAgent(
                logs_dir=logs_dir,
                poo_bin=str(fake_bin),
                settings=str(settings),
            )
            log_lines = [
                json.dumps({"record": "usage", "input_tokens": 123, "output_tokens": 45}),
                json.dumps({"record": "run.settled", "reason": "finished", "turns": 1}),
            ]
            env = FakeEnvironment(log_lines=log_lines, mount_logs_dir=None)
            ctx = FakeContext()

            asyncio.run(agent.setup(env))
            asyncio.run(agent.run("Do the thing", env, ctx))

            self.assertEqual(len(env.downloads), 1)
            self.assertEqual(ctx.n_input_tokens, 123)
            self.assertEqual(ctx.n_output_tokens, 45)
            self.assertIn("--settings /logs/agent/settings.yaml", env.commands[-1][0])


if __name__ == "__main__":
    unittest.main()
