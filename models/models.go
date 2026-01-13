// Package models contains the data structures used by the analyzer.
package models

type Node struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ComponentType string   `json:"component_type"`
	FilePath      string   `json:"file_path"`
	RelativePath  string   `json:"relative_path"`
	DependsOn     []string `json:"depends_on"`
	SourceCode    string   `json:"source_code,omitempty"`
	StartLine     int      `json:"start_line"`
	EndLine       int      `json:"end_line"`
	HasDocstring  bool     `json:"has_docstring"`
	Docstring     string   `json:"docstring"`
	Parameters    []string `json:"parameters,omitempty"`
	NodeType      string   `json:"node_type,omitempty"`
	BaseClasses   []string `json:"base_classes,omitempty"`
	ClassName     string   `json:"class_name,omitempty"`
	DisplayName   string   `json:"display_name,omitempty"`
	ComponentID   string   `json:"component_id,omitempty"`
}

type CallRelationship struct {
	Caller           string `json:"caller"`
	Callee           string `json:"callee"`
	CallLine         int    `json:"call_line,omitempty"`
	IsResolved       bool   `json:"is_resolved"`
	RelationshipType string `json:"relationship_type,omitempty"`
}

type AnalysisResult struct {
	Nodes             []Node             `json:"nodes"`
	CallRelationships []CallRelationship `json:"call_relationships"`
}
