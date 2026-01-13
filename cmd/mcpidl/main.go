package main

import (
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

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

// MCPData represents a data type definition
type MCPData struct {
	Name   string
	Fields []MCPField
}

// MCPTool represents a complete MCP tool definition
type MCPTool struct {
	Name        string
	Description string
	Package     string
	Input       MCPStruct
	Output      MCPStruct
}

// MCPParsedResult represents the parsed result of MCP file
type MCPParsedResult struct {
	Tools   []*MCPTool
	Data    []*MCPData
	Package string
}

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Println("Usage: mcpidl <idl-file|dir/...> [output-file]")
		os.Exit(1)
	}

	inputPath := os.Args[1]

	// Check if input is directory/... pattern
	if strings.HasSuffix(inputPath, "/...") {
		// Extract directory part
		dir := strings.TrimSuffix(inputPath, "/...")
		// Find all .mcp files recursively
		mcpFiles, err := findAllMCPFiles(dir)
		if err != nil {
			fmt.Printf("Error finding .mcp files: %v\n", err)
			os.Exit(1)
		}

		if len(mcpFiles) == 0 {
			fmt.Printf("No .mcp files found in %s\n", dir)
			os.Exit(0)
		}

		// Generate code for each .mcp file
		for _, mcpFile := range mcpFiles {
			generateCodeForFile(mcpFile, "")
		}
	} else {
		// Generate code for single file
		outputFile := ""
		if len(os.Args) == 3 {
			outputFile = os.Args[2]
		}
		generateCodeForFile(inputPath, outputFile)
	}
}

// generateCodeForFile generates Go code for a single .mcp file
func generateCodeForFile(filePath, outputFile string) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file %s: %v\n", filePath, err)
		os.Exit(1)
	}

	tools, dataList, goPackage, err := ParseMCPTools(string(content))
	if err != nil {
		fmt.Printf("Error parsing IDL %s: %v\n", filePath, err)
		os.Exit(1)
	}

	// Generate Go code
	goCode := GenerateGoCode(tools, dataList, goPackage)

	// Determine output filename
	if outputFile == "" {
		// Generate filename: test.mcp -> test.mcpc.go
		baseName := strings.TrimSuffix(filePath, ".mcp")
		outputFile = baseName + ".mcpc.go"
	}

	err = os.WriteFile(outputFile, []byte(goCode), 0644)
	if err != nil {
		fmt.Printf("Error writing to file %s: %v\n", outputFile, err)
		os.Exit(1)
	}

	fmt.Printf("Go code generated successfully: %s\n", outputFile)
}

// findAllMCPFiles recursively finds all .mcp files in the given directory
func findAllMCPFiles(dir string) ([]string, error) {
	var mcpFiles []string

	// Walk through all files in the directory recursively
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			// Skip .git directory
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}

		// Check if file has .mcp extension
		if strings.HasSuffix(path, ".mcp") {
			mcpFiles = append(mcpFiles, path)
		}

		return nil
	})

	return mcpFiles, err
}

// ParseMCPTools parses the MCP tool IDL and returns parsed result
func ParseMCPTools(content string) ([]*MCPTool, []*MCPData, string, error) {
	lines := strings.Split(content, "\n")
	var tools []*MCPTool
	var dataList []*MCPData
	var currentTool *MCPTool
	var currentData *MCPData
	var goPackage string = "mcp" // Default package
	var currentSection string
	var currentStruct *MCPStruct
	var inDataSection bool

	for i, line := range lines {
		//fmt.Printf("Line %d: %q, currentTool: %v, currentData: %v, currentSection: %q, inDataSection: %v\n",
		//	i+1, line, currentTool != nil, currentData != nil, currentSection, inDataSection)

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
					return nil, nil, "", fmt.Errorf("line %d: invalid go_package format", i+1)
				}
				pkgValue = line[start+1 : end]
			} else {
				// Format: package pkg
				parts := strings.Fields(line)
				if len(parts) < 2 {
					return nil, nil, "", fmt.Errorf("line %d: invalid package format", i+1)
				}
				pkgValue = parts[1]
			}
			goPackage = pkgValue
			continue
		}

		// Check for data declaration with brace
		if strings.HasPrefix(line, "data ") && strings.HasSuffix(line, " {") {
			// Format: data system {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return nil, nil, "", fmt.Errorf("line %d: invalid data declaration", i+1)
			}
			// Extract data name (remove trailing brace)
			dataName := parts[1]
			dataName = strings.TrimSuffix(dataName, "{")
			// Create new data type
			currentData = &MCPData{
				Name: dataName,
			}
			dataList = append(dataList, currentData)
			inDataSection = true
			continue
		}

		if strings.HasPrefix(line, "data") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return nil, nil, "", fmt.Errorf("line %d: invalid data declaration", i+1)
			}
			// Create new data type
			currentData = &MCPData{
				Name: parts[1],
			}
			dataList = append(dataList, currentData)
			continue
		}

		if strings.HasPrefix(line, "tool") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return nil, nil, "", fmt.Errorf("line %d: invalid tool declaration", i+1)
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
				return nil, nil, "", fmt.Errorf("line %d: description before tool declaration", i+1)
			}
			// Extract description between quotes, handle multiline
			start := strings.Index(line, "\"")
			if start == -1 {
				return nil, nil, "", fmt.Errorf("line %d: invalid description format", i+1)
			}

			// Check if description ends on the same line
			end := strings.LastIndex(line, "\"")
			if end > start {
				// Single line description
				currentTool.Description = line[start+1 : end]
			} else {
				// Multiline description - read until closing quote
				var descriptionBuilder strings.Builder
				// Add the first line after opening quote
				descriptionBuilder.WriteString(line[start+1:])

				// Continue reading lines until closing quote
				for j := i + 1; j < len(lines); j++ {
					nextLine := lines[j]
					endQuote := strings.Index(nextLine, "\"")
					if endQuote != -1 {
						// Found closing quote, add up to that point
						descriptionBuilder.WriteString("\n")
						descriptionBuilder.WriteString(nextLine[:endQuote])
						// Update current line index to j since we've processed these lines
						i = j
						break
					}
					// No closing quote yet, add entire line
					descriptionBuilder.WriteString("\n")
					descriptionBuilder.WriteString(nextLine)
				}
				currentTool.Description = descriptionBuilder.String()
			}
			continue
		}

		if line == "input {" {
			if currentTool == nil {
				return nil, nil, "", fmt.Errorf("line %d: input before tool declaration", i+1)
			}
			currentSection = "input"
			currentTool.Input.Name = currentTool.Name + "Input"
			currentStruct = &currentTool.Input
			inDataSection = false
			continue
		}

		if line == "output {" {
			if currentTool == nil {
				return nil, nil, "", fmt.Errorf("line %d: output before tool declaration", i+1)
			}
			currentSection = "output"
			currentTool.Output.Name = currentTool.Name + "Output"
			currentStruct = &currentTool.Output
			inDataSection = false
			continue
		}

		if line == "{" {
			if currentData != nil {
				// Start of data fields section
				inDataSection = true
			}
			continue
		}

		if line == "}" {
			if currentSection != "" {
				currentSection = ""
				currentStruct = nil
				inDataSection = false
			} else if inDataSection {
				// End of data fields section
				inDataSection = false
				currentData = nil
			}
			continue
		}

		if currentStruct != nil {
			// Parse fields in input/output sections
			field, err := parseField(line)
			if err != nil {
				return nil, nil, "", fmt.Errorf("line %d: %w", i+1, err)
			}
			currentStruct.Fields = append(currentStruct.Fields, field)
		} else if inDataSection && currentData != nil {
			// Parse fields in data section
			//fmt.Printf("Parsing data field: %q\n", line)
			field, err := parseField(line)
			if err != nil {
				return nil, nil, "", fmt.Errorf("line %d: %w", i+1, err)
			}
			currentData.Fields = append(currentData.Fields, field)
			//fmt.Printf("Added field %q to data %q\n", field.Name, currentData.Name)
		}
	}

	return tools, dataList, goPackage, nil
}

// parseField parses a single field line and returns a MCPField
func parseField(line string) (MCPField, error) {
	var field MCPField

	// Split into parts, handling quoted description with escaped quotes
	var parts []string
	inQuote := false
	current := ""
	escaped := false

	for _, char := range line {
		if escaped {
			// Previous character was a backslash, this character is escaped
			current += string(char)
			escaped = false
			continue
		}

		switch char {
		case '\\':
			// Start of escape sequence
			current += string(char)
			escaped = true
		case '"':
			// Toggle quote state for quoted description
			inQuote = !inQuote
			current += string(char)
		case ' ':
			if inQuote {
				// Inside quote, keep space
				current += string(char)
			} else if current != "" {
				// Outside quote, add current part and reset
				parts = append(parts, current)
				current = ""
			}
		default:
			// Regular character, add to current part
			current += string(char)
		}
	}
	// Add the last part if any
	if current != "" {
		parts = append(parts, current)
	}

	if len(parts) < 3 {
		return field, fmt.Errorf("invalid field format: %s", line)
	}

	// Remove trailing colon from field name
	field.Name = strings.TrimSuffix(parts[0], ":")

	// Handle array types: [type] -> []type
	fieldType := parts[1]
	if strings.HasPrefix(fieldType, "[") && strings.HasSuffix(fieldType, "]") {
		// Extract the inner type and convert to golang slice syntax
		innerType := fieldType[1 : len(fieldType)-1]
		field.Type = "[]" + innerType
	} else {
		// Handle object type: object -> map[string]any
		if fieldType == "object" {
			field.Type = "map[string]any"
		} else {
			field.Type = fieldType
		}
	}

	// Extract description from quoted string (combine all parts after type that contain quotes)
	var descriptionBuilder strings.Builder
	inDescription := false
	for _, part := range parts[2:] {
		if !inDescription {
			if strings.Contains(part, "\"") {
				// Start of description
				inDescription = true
			}
		}
		if inDescription {
			descriptionBuilder.WriteString(part)
			descriptionBuilder.WriteString(" ")
		}
	}
	descriptionStr := strings.TrimSpace(descriptionBuilder.String())

	// Extract the content between the first and last quote
	if len(descriptionStr) > 0 {
		startQuote := strings.Index(descriptionStr, "\"")
		if startQuote != -1 {
			endQuote := strings.LastIndex(descriptionStr, "\"")
			if endQuote > startQuote {
				field.Description = descriptionStr[startQuote+1 : endQuote]
			}
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

// toCamelCase converts any case to CamelCase (proper for Go identifiers)
func toCamelCase(s string) string {
	// If already starts with uppercase, assume it's already CamelCase
	if len(s) > 0 && unicode.IsUpper(rune(s[0])) {
		return s
	}

	// Handle camelCase first - split on uppercase letters
	var parts []string
	current := ""
	for _, r := range s {
		if unicode.IsUpper(r) {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		}
		current += string(r)
	}
	if current != "" {
		parts = append(parts, current)
	}

	// If no uppercase letters found, check for snake_case
	if len(parts) == 1 {
		parts = strings.Split(s, "_")
	}

	// Convert each part to TitleCase and join
	for i, part := range parts {
		parts[i] = cases.Title(language.English).String(part)
	}
	return strings.Join(parts, "")
}

// toLowerCamelCase converts snake_case to camelCase (first letter lowercase)
func toLowerCamelCase(s string) string {
	camelCase := toCamelCase(s)
	if len(camelCase) > 0 {
		return strings.ToLower(string(camelCase[0])) + camelCase[1:]
	}
	return ""
}

// toSnakeCase converts camelCase or PascalCase to snake_case
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			result.WriteRune('_')
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}

// GenerateGoCode generates Go code from multiple MCPTool structs and MCPData definitions
func GenerateGoCode(tools []*MCPTool, dataList []*MCPData, goPackage string) string {
	var builder strings.Builder

	// Add generated file comment
	builder.WriteString("// Code generated by mcpidl. DO NOT EDIT.\n\n")
	builder.WriteString(fmt.Sprintf("package %s\n\n", goPackage))

	// Create a map of data type names to their CamelCase counterparts for quick lookup
	dataTypeMap := make(map[string]string)
	for _, data := range dataList {
		dataTypeMap[data.Name] = toCamelCase(data.Name)
	}

	// Generate data structs first (so they can be referenced by tool structs)
	for _, data := range dataList {
		// Generate data struct with CamelCase
		dataStructName := toCamelCase(data.Name)
		builder.WriteString(fmt.Sprintf("type %s struct {\n", dataStructName))
		for _, field := range data.Fields {
			// Convert field name to CamelCase
			fieldName := toCamelCase(field.Name)
			// Use pointer type for non-required fields
			fieldType := field.Type
			// Check if fieldType is a referenced data type and convert to CamelCase
			if camelType, exists := dataTypeMap[fieldType]; exists {
				fieldType = camelType
			}
			if !field.Required {
				fieldType = "*" + fieldType
			}
			// Convert JSON tag to snake_case with omitempty for non-required fields
			jsonTag := toSnakeCase(field.Name)
			if !field.Required {
				jsonTag += ",omitempty"
			}
			builder.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\" jsonschema:\"%s\"`\n",
				fieldName, fieldType, jsonTag, field.Description))
		}
		builder.WriteString("}\n\n")

		// Generate getter methods for data struct
		generateGetterMethods(&builder, dataStructName, data.Fields, dataTypeMap)
	}

	// Generate constants for each tool
	for _, tool := range tools {
		// Generate tool name constant: CamelCase(tool.Name) + "Tool"
		camelCaseName := toCamelCase(tool.Name)
		toolNameConst := camelCaseName
		// Only add "Tool" suffix if it doesn't already end with "Tool"
		if !strings.HasSuffix(camelCaseName, "Tool") {
			toolNameConst = camelCaseName + "Tool"
		}
		// Convert tool.Name to snake_case
		snakeCaseToolName := toSnakeCase(tool.Name)
		builder.WriteString(fmt.Sprintf("const %s string = \"%s\"\n\n", toolNameConst, snakeCaseToolName))

		// Generate description constant: CamelCase(tool.Name) + "Description"
		descConstName := camelCaseName + "Description"
		// Escape only unescaped double quotes, preserve already escaped quotes
		var escapedDesc strings.Builder
		for i := 0; i < len(tool.Description); i++ {
			char := tool.Description[i]
			if char == '"' {
				// Check if this quote is already escaped
				if i > 0 && tool.Description[i-1] == '\\' {
					// Already escaped, write as-is
					escapedDesc.WriteByte(char)
				} else {
					// Need to escape this quote
					escapedDesc.WriteByte('\\')
					escapedDesc.WriteByte(char)
				}
			} else {
				// Write other characters as-is
				escapedDesc.WriteByte(char)
			}
		}
		builder.WriteString(fmt.Sprintf("const %s string = \"%s\"\n\n", descConstName, escapedDesc.String()))
	}

	// Generate structs for each tool
	for _, tool := range tools {
		// Generate input struct with proper CamelCase
		// tool.Input.Name is like "LaunchInput", so we need to ensure proper camel case
		inputStructName := toCamelCase(tool.Name) + "Input"
		builder.WriteString(fmt.Sprintf("type %s struct {\n", inputStructName))
		for _, field := range tool.Input.Fields {
			// Convert field name to CamelCase
			fieldName := toCamelCase(field.Name)
			// Use pointer type for non-required fields
			fieldType := field.Type
			// Check if fieldType is a referenced data type and convert to CamelCase
			if camelType, exists := dataTypeMap[fieldType]; exists {
				fieldType = camelType
			}
			if !field.Required {
				fieldType = "*" + fieldType
			}
			// Convert JSON tag to snake_case with omitempty for non-required fields
			jsonTag := toSnakeCase(field.Name)
			if !field.Required {
				jsonTag += ",omitempty"
			}
			builder.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\" jsonschema:\"%s\"`\n",
				fieldName, fieldType, jsonTag, field.Description))
		}
		builder.WriteString("}\n\n")

		// Generate getter methods for input struct
		generateGetterMethods(&builder, inputStructName, tool.Input.Fields, dataTypeMap)

		// Generate output struct with proper CamelCase
		outputStructName := toCamelCase(tool.Name) + "Output"
		builder.WriteString(fmt.Sprintf("type %s struct {\n", outputStructName))
		for _, field := range tool.Output.Fields {
			// Convert field name to CamelCase for Go conventions
			fieldName := toCamelCase(field.Name)
			// Use pointer type for non-required fields
			fieldType := field.Type
			// Check if fieldType is a referenced data type and convert to CamelCase
			if camelType, exists := dataTypeMap[fieldType]; exists {
				fieldType = camelType
			}
			if !field.Required {
				fieldType = "*" + fieldType
			}
			// Convert JSON tag to snake_case with omitempty for non-required fields
			jsonTag := toSnakeCase(field.Name)
			if !field.Required {
				jsonTag += ",omitempty"
			}
			builder.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\" jsonschema:\"%s\"`\n",
				fieldName, fieldType, jsonTag, field.Description))
		}
		builder.WriteString("}\n\n")

		// Generate getter methods for output struct
		generateGetterMethods(&builder, outputStructName, tool.Output.Fields, dataTypeMap)
	}

	// Format the code with gofmt
	formattedCode, err := format.Source([]byte(builder.String()))
	if err != nil {
		// If formatting fails, return the original code
		return builder.String()
	}

	return string(formattedCode)
}

// generateGetterMethods generates getter methods for all fields in a struct
func generateGetterMethods(builder *strings.Builder, structName string, fields []MCPField, dataTypeMap map[string]string) {
	for _, field := range fields {
		fieldName := toCamelCase(field.Name)
		fieldType := field.Type

		// Check if fieldType is a referenced data type and convert to CamelCase
		if camelType, exists := dataTypeMap[fieldType]; exists {
			fieldType = camelType
		}

		// Determine if the field is a pointer type in the struct
		// Non-required fields are pointer types in the generated struct
		isPointerField := !field.Required

		// Determine actual field type (pointer or non-pointer)
		actualType := fieldType

		// Generate getter method name: GetFieldName
		getterName := "Get" + fieldName

		// Generate getter method signature with value receiver instead of pointer receiver
		if isPointerField {
			// Getter for pointer field returns non-pointer value
			builder.WriteString(fmt.Sprintf("// %s returns the value of %s field\n", getterName, fieldName))
			builder.WriteString(fmt.Sprintf("func (s %s) %s() %s {\n", structName, getterName, actualType))
			builder.WriteString(fmt.Sprintf("\tif s.%s == nil {\n", fieldName))
			builder.WriteString(fmt.Sprintf("\t\tvar zero %s\n", actualType))
			builder.WriteString(fmt.Sprintf("\t\treturn zero\n"))
			builder.WriteString(fmt.Sprintf("\t}\n"))
			builder.WriteString(fmt.Sprintf("\treturn *s.%s\n", fieldName))
			builder.WriteString(fmt.Sprintf("}\n\n"))
		} else {
			// Getter for non-pointer field returns the value directly
			builder.WriteString(fmt.Sprintf("// %s returns the value of %s field\n", getterName, fieldName))
			builder.WriteString(fmt.Sprintf("func (s %s) %s() %s {\n", structName, getterName, actualType))
			builder.WriteString(fmt.Sprintf("\treturn s.%s\n", fieldName))
			builder.WriteString(fmt.Sprintf("}\n\n"))
		}
	}
}
