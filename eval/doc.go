// Package eval is the eval harness: it runs poo against a task set, measures
// what happened, and reports the result.
//
// The harness sits at the top of the module and may import anything. It
// never runs a model in-process: a run is the poo binary as a subprocess, so
// what is measured is what a user would get.
//
// The pieces are:
//
//   - The catalog: the models (evals/models.yaml) and the tasks
//     (evals/tasks/<id>/task.json).
//   - The runner: task x model x seed, each in its own checkout under the
//     repository's .cache directory.
//   - The metrics: everything computed from a session log, which is the only
//     source of truth about a run.
//   - The report: JSON plus a markdown table, written under
//     evals/results/<date>-<git sha>.
//   - Compare: two reports, and the noise floor that decides whether a
//     difference is a regression.
//   - Replay: a recorded session, re-run through the current harness with a
//     scripted transport, and the diff of the tool-call sequence.
//   - Judge: a model-judged read of each transcript.
//
// Nothing here needs a live model: the tests use a fake poo binary, a
// scripted transport and an httptest server.
package eval
