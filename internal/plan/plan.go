package plan

import (
	"fmt"
	"strings"
)

type StepStatus string

const (
	StatusPending    StepStatus = "pending"
	StatusInProgress StepStatus = "in_progress"
	StatusDone       StepStatus = "done"
	StatusFailed     StepStatus = "failed"
)

type Step struct {
	Text   string     `json:"text"`
	Status StepStatus `json:"status"`
}

type State struct {
	Title string `json:"title"`
	Steps []Step `json:"steps"`
}

func NewState() *State {
	return &State{}
}

func (s *State) Set(title string, steps []string) {
	s.Title = strings.TrimSpace(title)
	s.Steps = make([]Step, 0, len(steps))
	for _, step := range steps {
		text := strings.TrimSpace(step)
		if text == "" {
			continue
		}
		s.Steps = append(s.Steps, Step{
			Text:   text,
			Status: StatusPending,
		})
	}
}

func (s *State) Update(index int, status StepStatus, text string) error {
	if index < 1 || index > len(s.Steps) {
		return fmt.Errorf("step index out of range: %d", index)
	}
	if !ValidStatus(status) {
		return fmt.Errorf("invalid step status: %s", status)
	}

	step := &s.Steps[index-1]
	step.Status = status
	if strings.TrimSpace(text) != "" {
		step.Text = strings.TrimSpace(text)
	}
	return nil
}

func (s *State) Clear() {
	s.Title = ""
	s.Steps = nil
}

func (s *State) Empty() bool {
	return strings.TrimSpace(s.Title) == "" && len(s.Steps) == 0
}

func (s *State) String() string {
	if s.Empty() {
		return "No active plan."
	}

	var b strings.Builder
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = "Untitled plan"
	}
	fmt.Fprintf(&b, "Plan: %s\n", title)
	for i, step := range s.Steps {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, step.Status, step.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

func ValidStatus(status StepStatus) bool {
	switch status {
	case StatusPending, StatusInProgress, StatusDone, StatusFailed:
		return true
	default:
		return false
	}
}
