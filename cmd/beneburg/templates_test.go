package main

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplatesParse(t *testing.T) {
	templatePath := filepath.Join("..", "..", "templates", "*")
	_, err := template.ParseGlob(templatePath)
	if err != nil {
		t.Fatalf("parse HTML templates: %v", err)
	}
}

func TestProfileFormRequiresExplicitSubmitButton(t *testing.T) {
	profileTemplatePath := filepath.Join("..", "..", "templates", "profile.gohtml")
	profileTemplate, err := os.ReadFile(profileTemplatePath)
	if err != nil {
		t.Fatalf("read profile template: %v", err)
	}

	profileTemplateText := string(profileTemplate)
	if !strings.Contains(profileTemplateText, `type="button" class="btn btn-primary mb-3" id="submitProfileFormButton"`) {
		t.Error("profile submit button must not be an implicit submit button")
	}
	if !strings.Contains(profileTemplateText, "profileForm.requestSubmit()") {
		t.Error("profile submit button must explicitly submit the form")
	}
}
