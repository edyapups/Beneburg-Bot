package main

import (
	"html/template"
	"path/filepath"
	"testing"
)

func TestTemplatesParse(t *testing.T) {
	templatePath := filepath.Join("..", "..", "templates", "*")
	_, err := template.ParseGlob(templatePath)
	if err != nil {
		t.Fatalf("parse HTML templates: %v", err)
	}
}
