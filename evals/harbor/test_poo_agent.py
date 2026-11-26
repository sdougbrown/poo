"""Unit tests for the Harbor poo adapter (no Harbor or container runtime required)."""

from __future__ import annotations

import asyncio
import json
import platform
import subprocess
import os
import tempfile
import unittest
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from unittest.mock import patch

import poo_agent as poo_agent_module
ARCH = "arm64" if platform.machine().lower() in ("arm64", "aarch64") else "amd64"

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


    def test_an_unknown_provider_keeps_the_whole_slug_as_the_model_id(self) -> None:
        # A router slug such as "customrouter/my-model" must reach the request
        # whole. Splitting it the way a known provider splits would silently
        # truncate the model name.
        with patch.dict(os.environ, {}, clear=True):
            ep, mid, _ = resolve_endpoint_and_model("customrouter/my-model", None, None, None)
            self.assertIsNone(ep)
            self.assertEqual(mid, "customrouter/my-model")

    def test_the_endpoint_environment_variable_wins_over_a_known_provider(self) -> None:
        # A caller who exports POO_ENDPOINT gets that endpoint, not the
        # provider's default, and POO_API_KEY_ENV supplies the key variable.
        with patch.dict(
            os.environ,
            {"POO_ENDPOINT": "http://custom:4000/v1", "POO_API_KEY_ENV": "HOME_KEY"},
            clear=True,
        ):
            ep, _, key = resolve_endpoint_and_model("openai/gpt-4o", None, None, None)
            self.assertEqual(ep, "http://custom:4000/v1")
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


    def test_existing_metadata_is_preserved(self) -> None:
        # The trial may carry keys set before the poo run. Dropping them would
        # silently delete another agent's report from the trial.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = self._agent(tmp_path)
            ctx = FakeContext()
            ctx.metadata = {"pre_existing": "value"}

            agent.populate_context_post_run(ctx, return_code=0)

            self.assertEqual(ctx.metadata["pre_existing"], "value")
            self.assertEqual(ctx.metadata["return_code"], 0)

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

    def test_a_settings_run_sends_no_model_flags_of_its_own(self) -> None:
        # poo rejects --context-limit, --api-key-env or --tools-mode without
        # --endpoint and --model-id, so a settings run must send none of them:
        # the file names its own model.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            settings = tmp_path / "settings.yaml"
            settings.write_text("version: 1\n", encoding="utf-8")
            agent = self._agent(tmp_path, settings=str(settings))

            args = agent.build_run_args()
            joined = " ".join(args)
            self.assertIn("--settings /logs/agent/settings.yaml", joined)
            for flag in ("--context-limit", "--endpoint", "--model-id", "--tools-mode", "--api-key-env"):
                self.assertNotIn(flag, joined)

    def test_a_settings_run_refuses_a_model_flag_given_alone(self) -> None:
        # Sending it would exit with a usage error inside the container, and
        # dropping it would silently ignore what the caller asked for.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            settings = tmp_path / "settings.yaml"
            settings.write_text("version: 1\n", encoding="utf-8")
            agent = self._agent(tmp_path, settings=str(settings), tools_mode="native")

            with self.assertRaises(ValueError) as ctx:
                agent.build_run_args()
            self.assertIn("tools_mode", str(ctx.exception))

    def test_a_settings_run_can_name_a_model_explicitly(self) -> None:
        # Naming the model makes the whole group legal, so an explicit flag
        # reaches the run instead of being dropped.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            settings = tmp_path / "settings.yaml"
            settings.write_text("version: 1\n", encoding="utf-8")
            agent = self._agent(
                tmp_path,
                settings=str(settings),
                endpoint="http://localhost:4000/v1",
                model_id="glm-5.3-flash",
                context_limit=524288,
                tools_mode="native",
            )

            joined = " ".join(agent.build_run_args())
            self.assertIn("--settings /logs/agent/settings.yaml", joined)
            self.assertIn("--endpoint http://localhost:4000/v1", joined)
            self.assertIn("--model-id glm-5.3-flash", joined)
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

    def test_both_paths_forward_the_same_flags_when_a_model_is_named(self) -> None:
        # One block decides the model flags, so the settings path and the plain
        # path must agree once the caller names a model. Without one, the
        # settings path sends none of them, because poo rejects the group alone.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            settings = tmp_path / "settings.yaml"
            settings.write_text("version: 1\n", encoding="utf-8")
            common = dict(
                endpoint="http://localhost:4000/v1",
                model_id="glm-5.3-flash",
                context_limit=524288,
                tools_mode="native",
                api_key_env="OPENAI_API_KEY",
            )

            with_settings = self._agent(tmp_path, settings=str(settings), **common)
            without = self._agent(tmp_path, model_name="openai/gpt-4o", **common)

            for agent in (with_settings, without):
                joined = " ".join(agent.build_run_args())
                self.assertIn("--endpoint http://localhost:4000/v1", joined)
                self.assertIn("--model-id glm-5.3-flash", joined)
                self.assertIn("--context-limit 524288", joined)
                self.assertIn("--tools-mode native", joined)
                self.assertIn("--api-key-env OPENAI_API_KEY", joined)


    def test_the_optional_flags_reach_the_command(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = self._agent(
                tmp_path,
                model_name="openai/gpt-4o",
                context_limit=65536,
                profile="search",
                max_output=2000,
                truncation_retries=1,
                context_strategy="summary",
                context_keep_recent=3,
                timeout="30m",
                append_system_prompt="You are a helpful assistant.",
            )

            joined = " ".join(agent.build_run_args())
            for expected in (
                "--profile search",
                "--max-output 2000",
                "--truncation-retries 1",
                "--context-strategy summary",
                "--context-keep-recent 3",
                "--timeout 30m",
                "--append-system-prompt You are a helpful assistant.",
            ):
                self.assertIn(expected, joined)

    def test_a_partial_model_is_refused(self) -> None:
        # The third refusal path: one of the pair without the other. Falling
        # through it would proceed with half a model.
        with tempfile.TemporaryDirectory() as tmp:
            agent = self._agent(Path(tmp), endpoint="http://localhost:4000/v1")
            with self.assertRaises(ValueError) as ctx:
                agent.build_run_args()
            self.assertIn("must be given together", str(ctx.exception))

def _built_binary() -> Path | None:
    """The static Linux poo binary, when this checkout has built one."""
    candidate = Path(__file__).resolve().parent / "bin" / f"poo-linux-{ARCH}"
    return candidate if candidate.exists() else None


@unittest.skipUnless(_built_binary() is not None, "evals/harbor/bin has no built poo binary")
class PooAgentCommandValidationTests(unittest.TestCase):
    """The command the adapter sends is one poo actually accepts.

    poo exits 2 when it refuses its arguments. Each test runs the real binary
    against an unreachable endpoint, so what it asserts is that the command got
    past validation. Asserting the argument list instead is how a group of flags
    poo rejects passed the suite while every settings run died at startup.
    """

    USAGE_EXIT = 2

    def _run(self, agent: PooAgent, tmp_path: Path) -> subprocess.CompletedProcess[str]:
        argv = [
            str(_built_binary()) if arg == DEFAULT_BIN_TARGET else arg.replace("/logs/agent", str(tmp_path))
            for arg in agent.build_run_args()
        ]
        return subprocess.run(
            [*argv, "--", "say ok"],
            capture_output=True,
            text=True,
            timeout=120,
        )

    def _settings(self, tmp_path: Path) -> Path:
        path = tmp_path / "settings.yaml"
        path.write_text(
            "version: 1\n"
            "endpoints:\n"
            "  dead:\n"
            "    endpoint: http://127.0.0.1:1/v1\n"
            "models:\n"
            "  m:\n"
            "    endpoint: dead\n"
            "    model_id: unused\n"
            "    context_limit: 4096\n"
            "roles:\n"
            "  chat: m\n",
            encoding="utf-8",
        )
        return path

    def test_a_settings_run_is_accepted(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = PooAgent(
                logs_dir=tmp_path / "logs",
                poo_bin=str(_built_binary()),
                settings=str(self._settings(tmp_path)),
            )
            proc = self._run(agent, tmp_path)
            self.assertNotEqual(proc.returncode, self.USAGE_EXIT, proc.stderr)

    def test_a_run_naming_a_model_is_accepted(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = PooAgent(
                logs_dir=tmp_path / "logs",
                poo_bin=str(_built_binary()),
                endpoint="http://127.0.0.1:1/v1",
                model_id="unused",
                context_limit=4096,
                tools_mode="native",
            )
            proc = self._run(agent, tmp_path)
            self.assertNotEqual(proc.returncode, self.USAGE_EXIT, proc.stderr)

    def test_a_run_with_the_optional_flags_is_accepted(self) -> None:
        # Presence in the arg list does not prove poo accepts a flag's value, so
        # this runs the binary with the whole optional set against a dead
        # endpoint and asserts it got past argument validation.
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            agent = PooAgent(
                logs_dir=tmp_path / "logs",
                poo_bin=str(_built_binary()),
                endpoint="http://127.0.0.1:1/v1",
                model_id="unused",
                context_limit=4096,
                profile="search",
                max_output=2000,
                truncation_retries=1,
                context_strategy="summary",
                context_keep_recent=3,
                timeout="30s",
                append_system_prompt="You are a helpful assistant.",
            )
            proc = self._run(agent, tmp_path)
            self.assertNotEqual(proc.returncode, self.USAGE_EXIT, proc.stderr)


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
