"""Harbor agent adapter for poo.

Runs the static Linux `poo` binary inside Harbor task containers (Terminal-Bench,
SWE-bench, etc.) and populates Harbor's AgentContext from poo's JSONL session log.

Usage:
    PYTHONPATH=evals/harbor harbor run \
        --dataset terminal-bench/terminal-bench@4.1.0 \
        --agent poo_agent:PooAgent \
        --model openai/gpt-4o \
        --ak context_limit=128000 \
        --n-concurrent 4
"""

from __future__ import annotations

import json
import os
import platform
import shlex
import subprocess
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

try:
    from harbor.agents.base import BaseAgent
    from harbor.environments.base import BaseEnvironment
    from harbor.models.agent.context import AgentContext
    from harbor.models.trial.paths import EnvironmentPaths
except ImportError:  # pragma: no cover - allows importing/testing without harbor installed

    class BaseAgent:  # type: ignore[no-redef]
        def __init__(
            self,
            logs_dir: Path,
            model_name: str | None = None,
            *args: Any,
            extra_env: dict[str, str] | None = None,
            **kwargs: Any,
        ) -> None:
            self.logs_dir = Path(logs_dir)
            self.model_name = model_name
            # Harbor's BaseAgent owns extra_env as a read-only property over
            # _extra_env. Mirroring that here keeps the stub faithful, so a
            # subclass that shadows it fails the tests instead of the run.
            self._extra_env: dict[str, str] = dict(extra_env) if extra_env else {}

        @property
        def extra_env(self) -> dict[str, str]:
            return dict(self._extra_env)

    BaseEnvironment = Any  # type: ignore[misc,assignment]
    AgentContext = Any  # type: ignore[misc,assignment]

    class EnvironmentPaths:  # type: ignore[no-redef]
        agent_dir = Path("/logs/agent")


# Well-known provider prefixes for Harbor's `--model provider/model` syntax.
# Explicit `--ak endpoint=...` and `--ak api_key_env=...` always take precedence.
KNOWN_PROVIDERS: dict[str, tuple[str, str]] = {
    "openai": ("https://api.openai.com/v1", "OPENAI_API_KEY"),
    "openrouter": ("https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"),
    "ollama": ("https://ollama.com/v1", "OLLAMA_API_KEY"),
    "groq": ("https://api.groq.com/openai/v1", "GROQ_API_KEY"),
    "together": ("https://api.together.xyz/v1", "TOGETHER_API_KEY"),
    "deepseek": ("https://api.deepseek.com/v1", "DEEPSEEK_API_KEY"),
    "xai": ("https://api.x.ai/v1", "XAI_API_KEY"),
}

DEFAULT_CONTEXT_LIMIT = 131072
DEFAULT_BIN_TARGET = "/usr/local/bin/poo"


@dataclass
class SessionSummary:
    """Metrics extracted from a poo JSONL session log."""

    input_tokens: int = 0
    output_tokens: int = 0
    cache_read_tokens: int | None = None
    turns: int = 0
    tool_calls: int = 0
    tool_errors: int = 0
    compactions: int = 0
    settle_reason: str = "unknown"
    settle_source: str = ""
    settle_seen: bool = False
    torn: bool = False
    skipped_lines: int = 0
    records: int = 0
    extra: dict[str, Any] = field(default_factory=dict)


def parse_session_log(path: Path) -> SessionSummary:
    """Parse a poo JSONL session log into token and execution metrics.

    Unknown fields are ignored, as eval/log.go ignores them.

    Two rules deliberately diverge from eval/log.go. An undecodable line is
    skipped wherever it sits and the parse continues, where the Go reader fails
    on a torn line that is not the last one; a killed write can be followed by
    further records, and a metrics reader that aborts loses the whole trial.
    Turns fall back to the usage records when the log has no settle record,
    which is the killed-run case: the same fallback eval/log.go's Turns()
    applies, skipping the requests a compaction made.
    """
    summary = SessionSummary()
    if not path.exists():
        return summary

    raw_lines = [line.strip() for line in path.read_text(encoding="utf-8", errors="replace").splitlines()]
    lines = [line for line in raw_lines if line]
    if not lines:
        return summary

    cache_reported = False
    cache_total = 0
    # usage_turns counts what eval/log.go's Turns() counts when a log has no
    # usable settle record: the requests the run made, not the ones a
    # compaction made. A compaction is a request and not a turn, and its usage
    # record is marked by its origin rather than by its request number, which is
    # the marker poo.IsCompactionRequest reads.
    usage_turns = 0

    for idx, line in enumerate(lines):
        is_last = idx == len(lines) - 1
        try:
            rec = json.loads(line)
        except json.JSONDecodeError:
            summary.torn = True
            summary.skipped_lines += 1
            if is_last:
                break
            continue

        if not isinstance(rec, dict):
            continue

        summary.records += 1
        kind = rec.get("record")

        if kind == "usage":
            summary.input_tokens += int(rec.get("input_tokens") or 0)
            summary.output_tokens += int(rec.get("output_tokens") or 0)
            if rec.get("origin") != "compaction":
                usage_turns += 1
            cache_val = rec.get("cache_read_tokens")
            if cache_val is not None:
                cache_reported = True
                cache_total += int(cache_val)
        elif kind == "tool.call":
            summary.tool_calls += 1
        elif kind == "tool.result":
            if rec.get("outcome") == "error":
                summary.tool_errors += 1
        elif kind == "compaction":
            summary.compactions += 1
        elif kind == "run.settled":
            summary.settle_reason = str(rec.get("reason") or "unknown")
            summary.turns = int(rec.get("turns") or 0)
            summary.settle_seen = True
            detail = rec.get("detail")
            if isinstance(detail, dict):
                summary.settle_source = str(detail.get("source") or "")

    if cache_reported:
        summary.cache_read_tokens = cache_total
    # eval/log.go's Turns() trusts a settle record only when its turn count is
    # positive, so a settle record that carries none falls back the same way a
    # missing one does.
    if not summary.settle_seen or summary.turns <= 0:
        summary.turns = usage_turns

    return summary


def default_poo_binary() -> Path:
    """Resolve the default static Linux poo binary path."""
    env_bin = os.environ.get("POO_HARBOR_BIN")
    if env_bin:
        return Path(env_bin)

    machine = platform.machine().lower()
    arch = "arm64" if machine in ("arm64", "aarch64") else "amd64"
    return Path(__file__).resolve().parent / "bin" / f"poo-linux-{arch}"


def resolve_endpoint_and_model(
    model_name: str | None,
    endpoint: str | None,
    model_id: str | None,
    api_key_env: str | None,
) -> tuple[str | None, str | None, str | None]:
    """Resolve (endpoint, model_id, api_key_env) from Harbor's --model and kwargs.

    Explicit kwargs (or POO_ENDPOINT / POO_API_KEY_ENV env vars) always win.
    If only Harbor's `provider/model` is supplied, known providers are mapped
    automatically.
    """
    resolved_endpoint = endpoint or os.environ.get("POO_ENDPOINT")
    resolved_model_id = model_id
    resolved_api_key_env = api_key_env or os.environ.get("POO_API_KEY_ENV")

    if model_name:
        if "/" in model_name:
            provider, rest = model_name.split("/", 1)
            provider_key = provider.lower()
            if provider_key in KNOWN_PROVIDERS:
                default_ep, default_key_env = KNOWN_PROVIDERS[provider_key]
                resolved_endpoint = resolved_endpoint or default_ep
                resolved_api_key_env = resolved_api_key_env or default_key_env
                resolved_model_id = resolved_model_id or rest
            else:
                # Unknown provider prefix (for example openrouter model slugs or a custom router)
                resolved_model_id = resolved_model_id or model_name
        else:
            resolved_model_id = resolved_model_id or model_name

    return resolved_endpoint, resolved_model_id, resolved_api_key_env


class PooAgent(BaseAgent):
    """Harbor adapter that uploads a static Linux `poo` binary and runs `poo run`."""

    def __init__(
        self,
        logs_dir: Path,
        model_name: str | None = None,
        *,
        poo_bin: str | None = None,
        settings: str | None = None,
        endpoint: str | None = None,
        model_id: str | None = None,
        context_limit: int | None = None,
        api_key_env: str | None = None,
        tools_mode: str | None = None,
        reasoning_effort: str | None = None,
        max_turns: int | None = None,
        max_output: int | None = None,
        truncation_retries: int | None = None,
        profile: str = "code",
        context_strategy: str | None = None,
        context_keep_recent: int | None = None,
        timeout: str | None = None,
        append_system_prompt: str | None = None,
        **kwargs: Any,
    ) -> None:
        super().__init__(logs_dir=logs_dir, model_name=model_name, **kwargs)
        self.poo_bin = Path(poo_bin) if poo_bin else default_poo_binary()
        self.settings_path = Path(settings) if settings else None
        self.endpoint, self.model_id, self.api_key_env = resolve_endpoint_and_model(
            model_name=model_name,
            endpoint=endpoint,
            model_id=model_id,
            api_key_env=api_key_env,
        )
        self.context_limit = (
            int(context_limit)
            if context_limit is not None
            else (None if self.settings_path else DEFAULT_CONTEXT_LIMIT)
        )
        self.tools_mode = tools_mode
        self.reasoning_effort = reasoning_effort
        self.max_turns = int(max_turns) if max_turns is not None else None
        self.max_output = int(max_output) if max_output is not None else None
        self.truncation_retries = (
            int(truncation_retries) if truncation_retries is not None else None
        )
        self.profile = profile
        self.context_strategy = context_strategy
        self.context_keep_recent = (
            int(context_keep_recent) if context_keep_recent is not None else None
        )
        self.timeout = timeout
        self.append_system_prompt = append_system_prompt
        self._cached_version: str | None = None

    @staticmethod
    def name() -> str:
        return "poo"

    def version(self) -> str | None:
        if self._cached_version is not None:
            return self._cached_version
        if not self.poo_bin.exists():
            return None
        try:
            proc = subprocess.run(
                [str(self.poo_bin), "version"],
                capture_output=True,
                text=True,
                timeout=5,
                check=False,
            )
            if proc.returncode == 0 and proc.stdout.strip():
                self._cached_version = proc.stdout.strip().splitlines()[0]
                return self._cached_version
        except (OSError, subprocess.TimeoutExpired):
            # A probe that times out is a SubprocessError, not an OSError. It is
            # called while the trial's metadata is being written, so letting it
            # escape would lose the report of a run that already happened.
            pass
        return None

    def _agent_dir(self) -> str:
        return str(EnvironmentPaths.agent_dir)

    def _container_log_path(self) -> str:
        return f"{self._agent_dir()}/session.jsonl"

    def _container_settings_path(self) -> str:
        suffix = self.settings_path.suffix if self.settings_path else ".yaml"
        return f"{self._agent_dir()}/settings{suffix}"

    def _container_instruction_path(self) -> str:
        return f"{self._agent_dir()}/instruction.txt"

    def build_run_args(self) -> list[str]:
        """Build the `poo run` CLI argument list (excluding the instruction)."""
        args: list[str] = [
            DEFAULT_BIN_TARGET,
            "run",
            "--ephemeral",
            "--permission-unmatched",
            "allow",
            "--permission-fallback",
            "allow",
            "--log",
            self._container_log_path(),
        ]

        if self.profile:
            args.extend(["--profile", self.profile])

        if self.settings_path is not None:
            args.extend(["--settings", self._container_settings_path()])
        else:
            if not self.endpoint or not self.model_id:
                raise ValueError(
                    "PooAgent requires either `settings` (--ak settings=...) or "
                    "both `endpoint` and `model_id` (via --model provider/model or "
                    "--ak endpoint=... --ak model_id=...)."
                )
            args.extend(["--endpoint", self.endpoint, "--model-id", self.model_id])

        # One forwarding block for both paths. A flag appended in only one
        # branch works with --ak settings=... and is dropped without it, which
        # is the bug this block exists to prevent.
        #
        # A settings file names the model and its window, and an explicit flag
        # still wins over it, so an explicit value reaches the run either way.
        args.extend(
            [
                "--context-limit",
                str(self.context_limit or DEFAULT_CONTEXT_LIMIT),
            ]
        )
        if self.api_key_env:
            args.extend(["--api-key-env", self.api_key_env])
        if self.tools_mode:
            args.extend(["--tools-mode", self.tools_mode])

        if self.reasoning_effort:
            args.extend(["--reasoning-effort", self.reasoning_effort])
        if self.max_turns is not None and self.max_turns > 0:
            args.extend(["--max-turns", str(self.max_turns)])
        if self.max_output is not None and self.max_output > 0:
            args.extend(["--max-output", str(self.max_output)])
        if self.truncation_retries is not None and self.truncation_retries > 0:
            args.extend(["--truncation-retries", str(self.truncation_retries)])
        if self.context_strategy:
            args.extend(["--context-strategy", self.context_strategy])
        if self.context_keep_recent is not None and self.context_keep_recent > 0:
            args.extend(["--context-keep-recent", str(self.context_keep_recent)])
        if self.timeout:
            args.extend(["--timeout", self.timeout])
        if self.append_system_prompt:
            args.extend(["--append-system-prompt", self.append_system_prompt])

        return args

    def build_run_env(self) -> dict[str, str]:
        """Collect environment variables to pass into the container execution."""
        env: dict[str, str] = dict(self.extra_env)
        if self.api_key_env and self.api_key_env in os.environ:
            env[self.api_key_env] = os.environ[self.api_key_env]
        return env

    async def setup(self, environment: BaseEnvironment) -> None:
        """Upload the static Linux poo binary (and optional config) into the container."""
        if not self.poo_bin.exists():
            raise FileNotFoundError(
                f"Static Linux poo binary not found at {self.poo_bin}. "
                "Build it first with: evals/harbor/build-bin.sh"
            )

        agent_dir = self._agent_dir()
        await environment.exec(f"mkdir -p {shlex.quote(agent_dir)} /usr/local/bin")
        await environment.upload_file(
            source_path=self.poo_bin,
            target_path=DEFAULT_BIN_TARGET,
        )
        await environment.exec(f"chmod +x {shlex.quote(DEFAULT_BIN_TARGET)}")

        if self.settings_path is not None:
            if not self.settings_path.exists():
                raise FileNotFoundError(f"Settings file not found: {self.settings_path}")
            await environment.upload_file(
                source_path=self.settings_path,
                target_path=self._container_settings_path(),
            )

    async def run(
        self,
        instruction: str,
        environment: BaseEnvironment,
        context: AgentContext,
    ) -> None:
        """Execute `poo run` inside the container and populate Harbor's AgentContext."""
        self.logs_dir.mkdir(parents=True, exist_ok=True)

        # Write the instruction to a file and upload it so arbitrarily large or
        # quote-heavy task prompts never hit ARG_MAX or shell-quoting edge cases.
        local_instruction = self.logs_dir / "instruction.txt"
        local_instruction.write_text(instruction, encoding="utf-8")
        container_instruction = self._container_instruction_path()
        await environment.upload_file(
            source_path=local_instruction,
            target_path=container_instruction,
        )

        cmd_args = self.build_run_args()
        quoted_cmd = " ".join(shlex.quote(arg) for arg in cmd_args)
        shell_cmd = f'{quoted_cmd} -- "$(cat {shlex.quote(container_instruction)})"'

        result = await environment.exec(
            command=shell_cmd,
            env=self.build_run_env(),
        )

        if getattr(result, "stdout", None):
            (self.logs_dir / "stdout.txt").write_text(result.stdout, encoding="utf-8")
        if getattr(result, "stderr", None):
            (self.logs_dir / "stderr.txt").write_text(result.stderr, encoding="utf-8")

        # Harbor bind-mounts /logs/agent -> self.logs_dir on local Docker/Podman,
        # but remote sandboxes (Modal/Daytona) can require an explicit download
        local_log = self.logs_dir / "session.jsonl"
        if not local_log.exists() and hasattr(environment, "download_file"):
            try:
                await environment.download_file(
                    source_path=self._container_log_path(),
                    target_path=local_log,
                )
            except Exception:
                pass

        self.populate_context_post_run(context, return_code=getattr(result, "return_code", None))

    def populate_context_post_run(
        self,
        context: AgentContext,
        *,
        return_code: int | None = None,
    ) -> None:
        """Populate Harbor's AgentContext with token counts and poo run metadata.

        The metadata is written even when the log holds no records. A run that
        died before writing its first record — a refused setting, a bad
        tools mode, a container timeout, or a log that could not be downloaded —
        then reports its return code and a `no-log` settle reason instead of
        looking like a trial that did nothing.
        """
        summary = parse_session_log(self.logs_dir / "session.jsonl")

        if summary.records > 0:
            context.n_input_tokens = summary.input_tokens
            context.n_output_tokens = summary.output_tokens
            if summary.cache_read_tokens is not None:
                context.n_cache_tokens = summary.cache_read_tokens

        existing_meta = getattr(context, "metadata", None)
        meta: dict[str, Any] = dict(existing_meta) if isinstance(existing_meta, dict) else {}
        meta.update(
            {
                "poo_version": self.version(),
                "turns": summary.turns,
                "tool_calls": summary.tool_calls,
                "tool_errors": summary.tool_errors,
                "compactions": summary.compactions,
                "settle_reason": summary.settle_reason if summary.records else "no-log",
                "settle_source": summary.settle_source,
                "log_torn": summary.torn,
                "skipped_lines": summary.skipped_lines,
                "log_records": summary.records,
                "return_code": return_code,
            }
        )
        context.metadata = meta
