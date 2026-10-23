# The sandbox

Poo runs the commands a model asks for through an **executor**. The
executor decides where the command runs. There are two:

| Executor | Package | What it contains |
|---|---|---|
| `local` (default) | `poo/tool/bash` | nothing: the command runs in poo's own process space, and it can reach anything poo can |
| `bwrap` | `poo/exec/bwrap` | a bubblewrap sandbox: a namespace of its own, with a filesystem of its own |

The `bwrap` executor is Linux-only, and it is opt-in: a binary links the
executors it wants, and a session picks one by name. A name the binary
does not link is a configuration error, and so is a `bwrap` that cannot
start. Neither falls back to the local executor.

## What the `bwrap` executor contains

The command runs in a bubblewrap sandbox with:

- **Every namespace it can have.** The command does not see the host's
  processes, its cgroups or its host name.
- **A fresh `/proc`, `/dev` and `/tmp`.** The command sees its own
  processes and nobody else's.
- **The system directories, read-only:** `/usr`, `/bin`, `/lib`,
  `/lib64` and `/etc` by default. The settings may name a different
  list.
- **The working directory, read-write.** It is the only place the
  command can write, beside the extra read-write binds the settings
  name.
- **The network**, unless the settings cut it
  (`tools.bwrap.network: false`). The network is the host's, and with
  it the resolver's directory (`/run/systemd/resolve`) is mounted
  read-only, because `/etc/resolv.conf` is often a link into it.

`$HOME` is not mounted. The operator's files are not the command's,
unless a bind names them.

### Protected paths

A protected path inside a bound directory is masked: a directory behind
an empty tmpfs, a file behind an empty device. The executor asks the
matcher about every bind, so this covers the working directory, the
read-only system directories and the extra read-write binds alike. The
command reads nothing where the configuration protects something. A
protected path outside every bound directory is not in the sandbox at
all. The expansion of a bind is reused by the commands that follow, so a
large read-only bind is walked once, not once per command.

The `local` executor cannot do this. Protected paths stop the file
tools from wandering; with the `local` executor, `bash` reaches
everything the process can.

The paths are the settings' `tools.protected_paths`, and
`examples/code.settings.yaml` shows the setting with a commented
example: the operator's own secrets, protected from a model that goes
looking.

### What ends the sandbox

The sandbox ends when the command ends, and the kernel ends a
namespace when its init dies. A descendant that leaves the process
group (`setsid`, a double fork into a new session) therefore dies with
the call, which is the escape the `local` executor cannot close.

A background job runs in a sandbox of its own, and `cancel_job` or
`session/close` stops it the same way.

## Registered CLIs

A `call_cli` call is one of the operator's registered CLIs, and it runs
where a bash command runs: through the session's executor, with the
sandbox's binds and the protected paths it masks.

- **The command is the one its manifest names**, and the executor wraps
  it the way it wraps a shell command. A `bwrap` session runs it in the
  sandbox, and a protected path is masked there.
- **Its directory** is the manifest's when the manifest fixes one, and
  the session's working directory otherwise.
- **Its environment** is the session's environment, already scrubbed: the
  benign names the CLI needs (`PATH`, `HOME`, `LANG`, `LC_ALL`, `TMPDIR`)
  come from it, then the variables the manifest declares, with an
  `env:NAME` reference resolved against the scrubbed environment. A name
  the scrub removed, or a `tools.env.passthrough` list left out, is a
  reference the call cannot honour, and it fails naming the variable.
- **Its arguments** were checked against the manifest's schema before the
  process started, so a malformed call never runs. A value that begins
  with a dash is refused when the manifest passes it as an argument of
  its own, because a command whose flag takes no value would read it as
  another flag; bind it `equals` to pass a value that looks like one.
- **Its lifetime** is the plan's timeout, enforced by poo, and the run's
  context: a timeout, an interrupt, a cancel, a close or
  `--no-wait-jobs` stops the CLI and everything it started in its process
  group.
- **The output is bash's envelope**: both streams, the exit status, null
  for a killed process, and the partial output of a timeout.

The `local` executor still cannot hide what it does not own: a CLI it
runs can read poo's own environment through `/proc/$PPID/environ`,
because the process that started it is poo. The `bwrap` executor closes
that, because the CLI's parent is inside the sandbox's pid namespace.
Scrubbing stops a manifest reference; only isolation stops procfs.

## What the `bwrap` executor does not contain

**It is not a security boundary against hostile code.**

- **The kernel is the host's.** The sandbox is a namespace, not a
  kernel: a kernel bug is reachable from inside it.
- **The network is the host's**, unless it is cut. With the network on,
  the command can reach anything the host can reach, and the host can
  reach the command's ports.
- **The user is the host's user**, in a user namespace. The files the
  user owns are readable whenever they are bound.
- **A bind is a hole.** A read-write bind hands the command the
  directory it names, whatever is in it.

The sandbox is for keeping a model's commands inside the task: the
working directory, the fixtures, and nothing else. It is not for
running code you do not trust.

## Running all of poo inside a sandbox

The executor keeps the model's commands apart from poo. When the whole
process has to be contained -- a run on a shared machine, a run that
must not see the operator's files at all -- run poo itself inside
bubblewrap or a container. That is the portable option, and the only
one on macOS, where namespaces are not available to an unprivileged
process.

A whole-process sandbox for a poo run:

```sh
bwrap \
  --unshare-all --share-net --die-with-parent --new-session \
  --ro-bind /usr /usr --symlink usr/bin /bin --symlink usr/sbin /sbin \
  --symlink usr/lib /lib --symlink usr/lib64 /lib64 \
  --ro-bind /etc /etc \
  --ro-bind-try /run/systemd/resolve /run/systemd/resolve \
  --proc /proc --dev /dev --tmpfs /tmp \
  --bind "$PWD" /work \
  --chdir /work \
  --setenv HOME /tmp \
  -- poo run --working-dir /work --log /work/session.jsonl "..."
```

The container is the same idea with a different tool:

```sh
docker run --rm -it --network none -v "$PWD:/work" -w /work ghcr.io/sdougbrown/poo \
  poo run --working-dir /work --log /work/session.jsonl "..."
```

The eval harness does the first of these: `poo-eval run --sandbox bwrap`
wraps the whole `poo` process, and `poo-eval run --executor bwrap`
wraps the model's commands in a sandbox of their own, bound to the
task's workdir. The two are layers, not alternatives: a run may use
both, and each closes what the other leaves open.
