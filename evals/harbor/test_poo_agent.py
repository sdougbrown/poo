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

import poo_agent as poo_agent_module
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

    def test_tolerates_a_torn_line_before_the_end(self) -> None:
        # A killed write can be followed by more records, so a torn line is not
        # always the last one. The parse must skip it and keep counting rather
        # than abort, which is what killed a whole job once.
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "session.jsonl"
            log_path.write_text(
                json.dumps({"record": "usage", "input_tokens": 500, "output_tokens": 50})
                + "\n"
                + '{"record": "usage", "input_tok'
                + "\n"
                + json.dumps({"record": "usage", "input_tokens": 300, "output_tokens": 30})
                + "\n",
                encoding="utf-8",
            )
            summary = parse_session_log(log_path)
            self.assertEqual(summary.input_tokens, 800)
            self.assertEqual(summary.output_tokens, 80)
            self.assertTrue(summary.torn)
            self.assertEqual(summary.skipped_lines, 1)


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

            # clear=True: a host POO_ENDPOINT or POO_API_KEY_ENV would change the
            # resolution this test is pinning.
            with patch.dict(os.environ, {"OPENAI_API_KEY": "sk-test"}, clear=True):
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


class PooAgentMetadataTests(unittest.TestCase):
    """The metadata a trial reports when the run did not go cleanly."""

    def _agent(self, tmp_path: Path, **kwargs: Any) -> PooAgent:
        fake_bin = tmp_path / "poo-linux-amd64"
        fake_bin.write_bytes(b"#!/bin/sh\necho dev\n")
        return PooAgent(
            logs_dir=tmp_path / "logs",
            model_name="openai/gpt-4o",
            poo_bin=str(fake_bin),
            context_limit=65536,
            **kwargs,
        )

    def test_a_log_with_no_records_still_reports_the_return_code(self) -> None:
        # A run that died before writing its first record must not look like a
        # trial that did nothing: the return code and a no-log marker are the
        # only evidence the harness gets.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = self._agent(tmp_path)
            ctx = FakeContext()

            agent.populate_context_post_run(ctx, return_code=2)

            self.assertIsNotNone(ctx.metadata)
            self.assertEqual(ctx.metadata["return_code"], 2)
            self.assertEqual(ctx.metadata["settle_reason"], "no-log")
            self.assertEqual(ctx.metadata["log_records"], 0)
            self.assertIsNone(ctx.n_input_tokens)

    def test_turns_fall_back_to_the_usage_records_when_no_settle_exists(self) -> None:
        # A killed run writes no settle record. poo's own Turns() counts the
        # requests the run made in that case, skipping the ones a compaction
        # made, and the adapter must report the same number.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = self._agent(tmp_path)
            ctx = FakeContext()
            agent.logs_dir.mkdir(parents=True, exist_ok=True)
            lines = [
                json.dumps({"record": "usage", "request": 1, "input_tokens": 10, "output_tokens": 1}),
                json.dumps({"record": "usage", "request": 2, "input_tokens": 10, "output_tokens": 1}),
                # A compaction's request is marked by its origin, which is the
                # marker poo.IsCompactionRequest reads, so it is not a turn.
                json.dumps(
                    {"record": "usage", "origin": "compaction", "request": 0, "input_tokens": 10, "output_tokens": 1}
                ),
                json.dumps({"record": "usage", "request": 3, "input_tokens": 10, "output_tokens": 1}),
            ]
            (agent.logs_dir / "session.jsonl").write_text("\n".join(lines) + "\n", encoding="utf-8")

            agent.populate_context_post_run(ctx, return_code=None)

            self.assertIsNone(ctx.metadata["return_code"])
            self.assertEqual(ctx.metadata["turns"], 3)

    def test_a_settle_record_without_a_turn_count_still_falls_back(self) -> None:
        # eval/log.go's Turns() trusts a settle record only when its turn count
        # is positive. A recovery-written settle carries none, and the two
        # readers of one log must not disagree about how many turns it held.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = self._agent(tmp_path)
            ctx = FakeContext()
            agent.logs_dir.mkdir(parents=True, exist_ok=True)
            lines = [
                json.dumps({"record": "usage", "request": 1, "input_tokens": 1, "output_tokens": 1}),
                json.dumps({"record": "usage", "request": 2, "input_tokens": 1, "output_tokens": 1}),
                json.dumps({"record": "run.settled", "reason": "interrupted"}),
            ]
            (agent.logs_dir / "session.jsonl").write_text("\n".join(lines) + "\n", encoding="utf-8")

            agent.populate_context_post_run(ctx)

            self.assertEqual(ctx.metadata["turns"], 2)
            self.assertEqual(ctx.metadata["settle_reason"], "interrupted")

    def test_the_settle_record_still_wins_over_the_fallback(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = self._agent(tmp_path)
            ctx = FakeContext()
            agent.logs_dir.mkdir(parents=True, exist_ok=True)
            lines = [
                json.dumps({"record": "usage", "request": i, "input_tokens": 1, "output_tokens": 1})
                for i in range(1, 6)
            ]
            lines.append(json.dumps({"record": "run.settled", "reason": "finished", "turns": 9}))
            (agent.logs_dir / "session.jsonl").write_text("\n".join(lines) + "\n", encoding="utf-8")

            agent.populate_context_post_run(ctx)

            self.assertEqual(ctx.metadata["turns"], 9)
            self.assertEqual(ctx.metadata["settle_reason"], "finished")


class PooAgentFlagTests(unittest.TestCase):
    """The flags the run is given, and the ones it must never be given."""

    def _agent(self, tmp_path: Path, **kwargs: Any) -> PooAgent:
        fake_bin = tmp_path / "poo-linux-amd64"
        fake_bin.write_bytes(b"#!/bin/sh\necho dev\n")
        return PooAgent(logs_dir=tmp_path / "logs", poo_bin=str(fake_bin), **kwargs)

    def test_the_instruction_is_not_inlined_into_the_command(self) -> None:
        # A task's own pkill -f can match a command line that carries the
        # instruction, so the instruction travels through a file.
        instruction = "Fix the bug in `foo.py` with 'quotes' & $vars."
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = self._agent(tmp_path, model_name="openai/gpt-4o", context_limit=65536)
            env = FakeEnvironment()
            ctx = FakeContext()

            with patch.dict(os.environ, {"OPENAI_API_KEY": "sk-test"}, clear=True):
                asyncio.run(agent.setup(env))
                asyncio.run(agent.run(instruction, env, ctx))

            run_cmd = env.commands[-1][0]
            self.assertNotIn("Fix the bug", run_cmd)
            self.assertIn('-- "$(cat /logs/agent/instruction.txt)"', run_cmd)

    def test_settings_mode_still_applies_an_explicit_flag(self) -> None:
        # A settings file names the model, but an explicit flag must reach the
        # run instead of being dropped silently.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            settings = tmp_path / "settings.yaml"
            settings.write_text("version: 1\n", encoding="utf-8")
            agent = self._agent(
                tmp_path,
                settings=str(settings),
                context_limit=524288,
                tools_mode="native",
            )

            args = agent.build_run_args()
            joined = " ".join(args)
            self.assertIn("--settings /logs/agent/settings.yaml", joined)
            self.assertIn("--context-limit 524288", joined)
            self.assertIn("--tools-mode native", joined)

    def test_a_run_with_no_model_and_no_settings_refuses_to_start(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = self._agent(Path(tmp))
            with self.assertRaises(ValueError):
                agent.build_run_args()

    def test_extra_env_reaches_the_run(self) -> None:
        # The stub base class in this module mirrors Harbor's read-only
        # extra_env property, so this also pins the shadowing bug that shipped
        # once: an adapter that assigns self.extra_env fails here, not at run time.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = self._agent(
                tmp_path,
                model_name="openai/gpt-4o",
                context_limit=65536,
                extra_env={"HARBOR_TEST": "1"},
            )
            env = FakeEnvironment()
            ctx = FakeContext()

            with patch.dict(os.environ, {"OPENAI_API_KEY": "sk-test"}, clear=True):
                asyncio.run(agent.setup(env))
                asyncio.run(agent.run("Do the thing", env, ctx))

            run_env = env.commands[-1][1]
            self.assertEqual(run_env.get("HARBOR_TEST"), "1")
            self.assertEqual(run_env.get("OPENAI_API_KEY"), "sk-test")


    def test_a_flag_probe_that_times_out_does_not_fail_the_run(self) -> None:
        # version() runs while the trial's metadata is written, after the
        # expensive in-container run. A probe that hangs must degrade to None
        # rather than lose the report.
        import subprocess as sp

        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = self._agent(tmp_path, model_name="openai/gpt-4o", context_limit=65536)
            with patch.object(poo_agent_module.subprocess, "run", side_effect=sp.TimeoutExpired("poo", 5)):
                self.assertIsNone(agent.version())

    def test_both_model_paths_forward_the_same_flags(self) -> None:
        # One forwarding block serves both paths. A flag appended in only one
        # branch works with --ak settings=... and is dropped without it.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            settings = tmp_path / "settings.yaml"
            settings.write_text("version: 1\n", encoding="utf-8")
            common = dict(context_limit=524288, tools_mode="native", api_key_env="OPENAI_API_KEY")

            with_settings = self._agent(tmp_path, settings=str(settings), **common)
            without = self._agent(
                tmp_path, model_name="openai/gpt-4o", endpoint="http://x/v1", model_id="m", **common
            )

            for agent in (with_settings, without):
                joined = " ".join(agent.build_run_args())
                self.assertIn("--context-limit 524288", joined)
                self.assertIn("--tools-mode native", joined)
                self.assertIn("--api-key-env OPENAI_API_KEY", joined)


def _harbor_installed() -> bool:
    try:
        import harbor.agents.base  # noqa: F401
    except ImportError:
        return False
    return True


@unittest.skipUnless(_harbor_installed(), "harbor is not installed")
class RealHarborBaseAgentTests(unittest.TestCase):
    """The adapter against Harbor's own BaseAgent, not this module's stub.

    Everything else here runs against the stub, which is how the extra_env
    shadowing bug shipped: the stub lacked the read-only property Harbor's base
    class exposes, so the collision only appeared at run time.
    """

    def test_the_adapter_constructs_and_keeps_its_env(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = PooAgent(
                logs_dir=Path(tmp) / "logs",
                model_name="openai/gpt-4o",
                endpoint="http://localhost:4000/v1",
                model_id="gpt-4o",
                context_limit=65536,
                extra_env={"HARBOR_TEST": "1"},
            )
            self.assertEqual(agent.extra_env.get("HARBOR_TEST"), "1")
            self.assertIn("--endpoint http://localhost:4000/v1", " ".join(agent.build_run_args()))


if __name__ == "__main__":
    unittest.main()
