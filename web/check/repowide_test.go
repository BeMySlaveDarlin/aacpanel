package check

import (
	"strings"
	"testing"
)

type repoWideShot struct {
	BaseLabel          string   `json:"baseLabel"`
	BaseTitle          string   `json:"baseTitle"`
	HeaderFeedPressed  bool     `json:"headerFeedPressed"`
	HeaderFilesPressed bool     `json:"headerFilesPressed"`
	FolderCrumb        string   `json:"folderCrumb"`
	TreeBackRoot       bool     `json:"treeBackRoot"`
	Wide               bool     `json:"wide"`
	Closed             bool     `json:"closed"`
	Opened             bool     `json:"opened"`
	ButtonText         string   `json:"buttonText"`
	FeedWithSidebar    bool     `json:"feedWithSidebar"`
	FolderCount        string   `json:"folderCount"`
	ChangedMarks       string   `json:"changedMarks"`
	TwoActionsOpened   bool     `json:"twoActionsOpened"`
	DefaultChanges     string   `json:"defaultChanges"`
	KeptFeed           bool     `json:"keptFeed"`
	KeptComposer       bool     `json:"keptComposer"`
	Selected           bool     `json:"selected"`
	FileLines          []string `json:"fileLines"`
	Wrap               bool     `json:"wrap"`
	Returned           bool     `json:"returned"`
	TreeMarks          string   `json:"treeMarks"`
	TreeOpened         string   `json:"treeOpened"`
	BackToFolder       bool     `json:"backToFolder"`
	BackToConversation bool     `json:"backToConversation"`
	Folded             bool     `json:"folded"`
	NoTabs             bool     `json:"noTabs"`
	ToolFileEntry      bool     `json:"toolFileEntry"`
}

func TestRepoFilesStayWithTheConversation(t *testing.T) {
	parallel(t)
	for _, shape := range []struct {
		name string
		run  func(*testing.T, string, any)
		wide bool
	}{
		{"phone", runFixture, false}, {"desk", runWideFixture, true},
	} {
		t.Run(shape.name, func(t *testing.T) {
			var got repoWideShot
			shape.run(t, "repowide.html", &got)
			if got.Wide != shape.wide {
				t.Fatalf("wrong screen shape: %+v", got)
			}
			if !got.Closed || !got.Opened || !got.FeedWithSidebar {
				t.Errorf("Files did not open beside the mounted feed: %+v", got)
			}
			if !got.TwoActionsOpened || got.DefaultChanges != "Changes" {
				t.Errorf("the header button and one file press did not open its changes: %+v", got)
			}
			if !got.KeptFeed || !got.KeptComposer {
				t.Error("opening a file discarded the feed or its composer")
			}
			if got.BaseLabel != "vs main" || !strings.Contains(got.BaseTitle, "project") {
				t.Errorf("the comparison base reads %q (%q)", got.BaseLabel, got.BaseTitle)
			}
			if !got.HeaderFilesPressed || shape.wide && got.HeaderFeedPressed {
				t.Error("the header must select Files, not Feed, while the file is in view")
			}
			if !strings.Contains(got.FolderCrumb, "pkg") || !got.TreeBackRoot {
				t.Error("the folder crumbs or the way up are missing from All files")
			}
			if !got.Selected {
				t.Error("the file in view has no selection in Files")
			}
			if len(got.FileLines) != 3 || got.FileLines[0] != "package pkg" {
				t.Errorf("File did not show the file contents: %v", got.FileLines)
			}
			if !got.Wrap {
				t.Error("the wrap control did not wrap the file")
			}
			if !got.Returned {
				t.Error("returning from the file did not restore the feed and keep Files open")
			}
			if got.TreeOpened != "pkg/env.go" {
				t.Errorf("the tree opened %q", got.TreeOpened)
			}
			if !got.NoTabs || got.ToolFileEntry {
				t.Error("file tabs or the tools entry still stand")
			}
			if shape.wide && !got.Folded {
				t.Error("Files did not close the sidebar")
			}
			if !shape.wide && (!got.BackToFolder || !got.BackToConversation) {
				t.Error("system back did not return through the same folder to the conversation")
			}
		})
	}
}

func TestRepoTreeAndChangedShowTheSameMarks(t *testing.T) {
	var got repoWideShot
	runWideFixture(t, "repowide.html", &got)
	if got.FolderCount != "2" {
		t.Errorf("the folder says %q changed files, want 2", got.FolderCount)
	}
	for _, row := range []string{got.ChangedMarks, got.TreeMarks} {
		for _, mark := range []string{"M", "+6", "−1"} {
			if !strings.Contains(row, mark) {
				t.Errorf("%q lacks %q", row, mark)
			}
		}
	}
	if !strings.Contains(got.ButtonText, "2 changed") {
		t.Errorf("the header says %q", got.ButtonText)
	}
}
