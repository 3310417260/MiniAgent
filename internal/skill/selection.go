package skill

import (
	"fmt"
	"strings"
)

const None = "none"

type Selection struct {
	Name   string
	Reason string
}

func (s *Selection) Clear() {
	s.Name = ""
	s.Reason = ""
}

func (s *Selection) Set(name string, reason string) {
	s.Name = strings.TrimSpace(name)
	s.Reason = strings.TrimSpace(reason)
}

func (s Selection) Empty() bool {
	return s.Name == ""
}

func (s Selection) UsesSkill() bool {
	return s.Name != "" && s.Name != None
}

func (s Selection) String() string {
	if s.Empty() {
		return "No skill selected."
	}
	if s.Reason == "" {
		return fmt.Sprintf("Selected skill: %s", s.Name)
	}
	return fmt.Sprintf("Selected skill: %s\nReason: %s", s.Name, s.Reason)
}
