package main

import (
	"fmt"
	"go/format"
	"os"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// MCPStruct represents a struct definition (input or output)
type MCPStruct struct {
	Name   string
	Fields []MCPField
}

// MCPField represents a field in a struct
type MCPField struct {
	Name        string
	Type        string
	Description string
	Required    bool
}

// MCPTool represents a complete MCP tool definition
type MCPTool struct {
	Name        string
	Description string
	Package     string
	Input       MCPStruct
	Output      MCPStruct
}

func main() {
	if len(os.Args) != 2 && len(os.Args) != 3 {
		fmt.Println("Usage: mcpidl <idl-file> [output-file]")
		os.Exit(1)
	}

	filePath := os.Args[1]
	content, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	tools, goPackage, err := ParseMCPTools(string(content))
	if err != nil {
		fmt.Printf("Error parsing IDL: %v\n", err)
		os.Exit(1)
	}

	// Generate Go code
	goCode := GenerateGoCode(tools, goPackage)

	// Write to file
	var outputFile string
	if len(os.Args) == 3 {
		outputFile = os.Args[2]
	} else {
		// Generate filename: test.mcp -> test.mcpc.go
		baseName := strings.TrimSuffix(filePath, ".mcp")
		outputFile = baseName + ".mcpc.go"
	}

	err = os.WriteFile(outputFile, []byte(goCode), 0644)
	if err != nil {
		fmt.Printf("Error writing to file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Go code generated successfully: %s\n", outputFile)
}

// ParseMCPTools parses the MCP tool IDL and returns multiple MCPTool structs and go_package
func ParseMCPTools(content string) ([]*MCPTool, string, error) {
	lines := strings.Split(content, "\n")
	var tools []*MCPTool
	var currentTool *MCPTool
	var goPackage string = "mcp" // Default package
	var currentSection string
	var currentStruct *MCPStruct

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Parse go_package at file level
		if strings.HasPrefix(line, "go_package") {
			// Extract go_package value - support both formats: go_package = "pkg" and package pkg
			var pkgValue string
			if strings.Contains(line, "=") {
				// Format: go_package = "pkg"
				start := strings.Index(line, "\"")
				end := strings.LastIndex(line, "\"")
				if start == -1 || end == -1 || start >= end {
					return nil, "", fmt.Errorf("line %d: invalid go_package format", i+1)
				}
				pkgValue = line[start+1 : end]
			} else {
				// Format: package pkg
				parts := strings.Fields(line)
				if len(parts) < 2 {
					return nil, "", fmt.Errorf("line %d: invalid package format", i+1)
				}
				pkgValue = parts[1]
			}
			goPackage = pkgValue
			continue
		}

		if strings.HasPrefix(line, "tool") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return nil, "", fmt.Errorf("line %d: invalid tool declaration", i+1)
			}
			// Create new tool
			currentTool = &MCPTool{
				Name: parts[1],
			}
			tools = append(tools, currentTool)
			continue
		}

		if strings.HasPrefix(line, "description") {
			if currentTool == nil {
				return nil, "", fmt.Errorf("line %d: description before tool declaration", i+1)
			}
			// Extract description between quotes
			start := strings.Index(line, "\"")
			end := strings.LastIndex(line, "\"")
			if start == -1 || end == -1 || start >= end {
				return nil, "", fmt.Errorf("line %d: invalid description format", i+1)
			}
			currentTool.Description = line[start+1 : end]
			continue
		}

		if line == "input {" {
			if currentTool == nil {
				return nil, "", fmt.Errorf("line %d: input before tool declaration", i+1)
			}
			currentSection = "input"
			currentTool.Input.Name = currentTool.Name + "Input"
			currentStruct = &currentTool.Input
			continue
		}

		if line == "output {" {
			if currentTool == nil {
				return nil, "", fmt.Errorf("line %d: output before tool declaration", i+1)
			}
			currentSection = "output"
			currentTool.Output.Name = currentTool.Name + "Output"
			currentStruct = &currentTool.Output
			continue
		}

		if line == "}" {
			if currentSection != "" {
				currentSection = ""
				currentStruct = nil
			}
			continue
		}

		if currentStruct != nil {
			field, err := parseField(line)
			if err != nil {
				return nil, "", fmt.Errorf("line %d: %w", i+1, err)
			}
			currentStruct.Fields = append(currentStruct.Fields, field)
		}
	}

	return tools, goPackage, nil
}

// parseField parses a single field line and returns a MCPField
func parseField(line string) (MCPField, error) {
	var field MCPField

	// Split into parts, handling quoted description
	var parts []string
	inQuote := false
	current := ""

	for _, char := range line {
		switch char {
		case '"':
			inQuote = !inQuote
			current += string(char)
		case ' ':
			if inQuote {
				current += string(char)
			} else if current != "" {
				parts = append(parts, current)
				current = ""
			}
		default:
			current += string(char)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}

	if len(parts) < 3 {
		return field, fmt.Errorf("invalid field format: %s", line)
	}

	// Remove trailing colon from field name
	field.Name = strings.TrimSuffix(parts[0], ":")
	field.Type = parts[1]

	// Extract description from quoted string
	for _, part := range parts[2:] {
		if strings.HasPrefix(part, "\"") {
			start := strings.Index(part, "\"")
			end := strings.LastIndex(part, "\"")
			if start != -1 && end != -1 && start < end {
				field.Description = part[start+1 : end]
			}
			break
		}
	}

	// Check if required
	for _, part := range parts {
		if part == "required" {
			field.Required = true
			break
		}
	}

	return field, nil
}

// toCamelCase converts snake_case to CamelCase
func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	for i, part := range parts {
		parts[i] = cases.Title(language.English).String(part)
	}
	return strings.Join(parts, "")
}

// GenerateGoCode generates Go code from multiple MCPTool structs
func GenerateGoCode(tools []*MCPTool, goPackage string) string {
	var builder strings.Builder

	// Add generated file comment
	builder.WriteString("// Code generated by mcpidl. DO NOT EDIT.\n\n")
	builder.WriteString(fmt.Sprintf("package %s\n\n", goPackage))

	// Generate structs for each tool
	for _, tool := range tools {
		// Generate input struct with TitleCase
		inputStructName := cases.Title(language.English).String(tool.Input.Name)
		builder.WriteString(fmt.Sprintf("type %s struct {\n", inputStructName))
		for _, field := range tool.Input.Fields {
			// Convert field name to CamelCase
			fieldName := toCamelCase(field.Name)
			// Use pointer type for non-required fields
			fieldType := field.Type
			if !field.Required {
				fieldType = "*" + fieldType
			}
			builder.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\" jsonschema:\"%s\"`\n",
				fieldName, fieldType, field.Name, field.Description))
		}
		builder.WriteString("}\n\n")

		// Generate output struct with same naming convention as input
		outputStructName := cases.Title(language.English).String(tool.Name) + "Output"
		builder.WriteString(fmt.Sprintf("type %s struct {\n", outputStructName))
		for _, field := range tool.Output.Fields {
			// Convert field name to CamelCase for Go conventions
			fieldName := toCamelCase(field.Name)
			// Use pointer type for non-required fields
			fieldType := field.Type
			if !field.Required {
				fieldType = "*" + fieldType
			}
			builder.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\" jsonschema:\"%s\"`\n",
				fieldName, fieldType, field.Name, field.Description))
		}
		builder.WriteString("}\n\n")
	}

	// Format the code with gofmt
	formattedCode, err := format.Source([]byte(builder.String()))
	if err != nil {
		// If formatting fails, return the original code
		return builder.String()
	}

	return string(formattedCode)
}
