package poo

import (
	_ "embed"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed jobs.md
var jobsSectionText string

// jobsSection is the model's guidance about background jobs.
//
// The section names the two tools the core registers for it. The
// cancel_job tool has no section of its own: the jobs tool's section
// names both.
var jobsSection = prompt.MustCompile("jobs", jobsSectionText, nil)

// JobsPromptSection is the model's guidance about background jobs, as
// the core's own jobs tool renders it.
func JobsPromptSection() string {
	return jobsSection.Render(prompt.Env{})
}

// PromptSection renders the section that tells the model about the
// background jobs.
//
// The section is the model's guidance, and it names the two tools the
// core registers for it. The cancel_job tool has no section of its
// own: the jobs tool's section names both.
func (t *JobTool) PromptSection(env prompt.Env) string {
	if t.name == CancelJobToolName {
		return ""
	}
	return jobsSection.Render(env)
}
