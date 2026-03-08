package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

// LSPClient wraps the LSP client functionality using Sourcegraph's libraries
type LSPClient struct {
	conn    *jsonrpc2.Conn
	ctx     context.Context
	cancel  context.CancelFunc
	hasInit bool
	mu      sync.RWMutex
}

// PositionConverter handles conversion between tree-sitter and LSP positions
type PositionConverter struct {
	text       string
	lines      []string
	lineStarts []int
}

// SymbolInfo represents symbol information returned from LSP
type SymbolInfo struct {
	Name          string       `json:"name"`
	Kind          int          `json:"kind"`
	Location      lsp.Location `json:"location"`
	ContainerName string       `json:"containerName,omitempty"`
}

// NewPositionConverter creates a new position converter
func NewPositionConverter(text string) *PositionConverter {
	lines := strings.Split(text, "\n")
	converter := &PositionConverter{
		text:  text,
		lines: lines,
	}
	converter.lineStarts = converter.computeLineStarts()
	return converter
}

func (pc *PositionConverter) computeLineStarts() []int {
	starts := []int{0}
	pos := 0
	for i, line := range pc.lines {
		if i == len(pc.lines)-1 {
			break
		}
		pos += len([]byte(line)) + 1 // +1 for \n
		starts = append(starts, pos)
	}
	return starts
}

func (pc *PositionConverter) TreeSitterToLSP(tsPoint map[string]int) map[string]int {
	row := tsPoint["row"]
	byteColumn := tsPoint["column"]

	if row >= len(pc.lines) {
		return map[string]int{"line": len(pc.lines) - 1, "character": 0}
	}

	lineText := pc.lines[row]
	lineBytes := []byte(lineText)

	if byteColumn > len(lineBytes) {
		byteColumn = len(lineBytes)
	}

	// Convert byte offset to UTF-16 character offset
	textUpToColumn := string(lineBytes[:byteColumn])
	utf16Character := len(utf16.Encode([]rune(textUpToColumn)))

	return map[string]int{
		"line":      row,
		"character": utf16Character,
	}
}

// NewLSPClient creates a new LSP client using Sourcegraph's jsonrpc2
func NewLSPClient() *LSPClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &LSPClient{
		ctx:     ctx,
		cancel:  cancel,
		hasInit: false,
	}
}

// StartGopls starts the gopls language server
func (client *LSPClient) StartGopls(serverPath string) error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.conn != nil {
		return fmt.Errorf("LSP client already started")
	}

	// Start gopls process
	cmd := exec.CommandContext(client.ctx, serverPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start gopls: %w", err)
	}

	// Create JSONRPC2 connection
	stream := jsonrpc2.NewBufferedStream(&readWriteCloser{
		Reader: stdout,
		Writer: stdin,
		Closer: stdin,
	}, jsonrpc2.VSCodeObjectCodec{})

	conn := jsonrpc2.NewConn(client.ctx, stream, &lspHandler{})
	client.conn = conn

	return nil
}

// readWriteCloser combines io.Reader, io.Writer, and io.Closer
type readWriteCloser struct {
	io.Reader
	io.Writer
	io.Closer
}

// lspHandler handles LSP notifications and requests
type lspHandler struct{}

func (h *lspHandler) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	// Handle LSP notifications and responses
	if req.Method == "window/logMessage" {
		var params lsp.LogMessageParams
		if err := json.Unmarshal(*req.Params, &params); err == nil {
			log.Printf("LSP Log [%d]: %s", params.Type, params.Message)
		}
	}
}

// Initialize sends initialize request to LSP server
func (client *LSPClient) Initialize(rootURI string) error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.conn == nil {
		return fmt.Errorf("LSP connection not established")
	}

	params := lsp.InitializeParams{
		RootURI: lsp.DocumentURI(rootURI),
		Capabilities: lsp.ClientCapabilities{
			TextDocument: lsp.TextDocumentClientCapabilities{},
		},
	}

	var result lsp.InitializeResult
	if err := client.conn.Call(client.ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}
	fmt.Println("initialize result", result)
	initializeResult := map[string]interface{}{}
	if err := client.conn.Notify(client.ctx, "initialized", &initializeResult); err != nil {
		return fmt.Errorf("initialized notification failed: %w", err)
	}
	fmt.Println("initializeResult", initializeResult)
	client.hasInit = true
	return nil
}

// DidOpen sends textDocument/didOpen notification
func (client *LSPClient) DidOpen(uri, languageID, text string) error {
	client.mu.RLock()
	defer client.mu.RUnlock()

	if client.conn == nil {
		return fmt.Errorf("LSP connection not established")
	}

	params := lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI:        lsp.DocumentURI(uri),
			LanguageID: languageID,
			Version:    1,
			Text:       text,
		},
	}

	return client.conn.Notify(client.ctx, "textDocument/didOpen", params)
}

// GetDefinition sends textDocument/definition request
func (client *LSPClient) GetDefinition(uri string, position lsp.Position) ([]lsp.Location, error) {
	client.mu.RLock()
	defer client.mu.RUnlock()

	if client.conn == nil {
		return nil, fmt.Errorf("LSP connection not established")

	}

	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": lsp.DocumentURI(uri),
		},
		"position": position,
	}
	var result []lsp.Location
	if err := client.conn.Call(client.ctx, "textDocument/definition", params, &result); err != nil {
		return nil, fmt.Errorf("definition request failed: %w", err)
	}
	return result, nil
}

// GetDocumentSymbols sends textDocument/documentSymbol request
func (client *LSPClient) GetDocumentSymbols(uri string) ([]lsp.SymbolInformation, error) {
	client.mu.RLock()
	defer client.mu.RUnlock()

	if client.conn == nil {
		return nil, fmt.Errorf("LSP connection not established")
	}

	params := lsp.DocumentSymbolParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: lsp.DocumentURI(uri)},
	}

	var result []lsp.SymbolInformation
	if err := client.conn.Call(client.ctx, "textDocument/documentSymbol", params, &result); err != nil {
		return nil, fmt.Errorf("document symbol request failed: %w", err)
	}

	return result, nil
}

// Shutdown gracefully shuts down the LSP client
func (client *LSPClient) Shutdown() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.conn == nil {
		return nil
	}

	// Send shutdown request
	var result interface{}
	if err := client.conn.Call(client.ctx, "shutdown", nil, &result); err != nil {
		log.Printf("Shutdown request failed: %v", err)
	}

	// Send exit notification
	if err := client.conn.Notify(client.ctx, "exit", nil); err != nil {
		log.Printf("Exit notification failed: %v", err)
	}

	// Close connection
	if err := client.conn.Close(); err != nil {
		log.Printf("Failed to close connection: %v", err)
	}

	client.cancel()
	client.conn = nil
	client.hasInit = false

	return nil
}

// findGoReferenceName finds the reference name for an imported item
func findGoReferenceName(sourceFilePath, importedItemPath string) (string, error) {
	importsMap, err := parseGoImports(sourceFilePath)
	if err != nil {
		return "", err
	}

	if len(importsMap) == 0 {
		return "", fmt.Errorf("在源文件 '%s' 中没有找到任何 import 语句", sourceFilePath)
	}

	for path, name := range importsMap {
		fmt.Printf("      - '%s' -> '%s'\n", path, name)
	}
	targetImportPath, err := getImportPathForFile(importedItemPath)
	if err != nil {
		return "", err
	}
	if refName, exists := importsMap[targetImportPath]; exists {
		if refName == "_" {
			return "", fmt.Errorf("包 '%s' 是一个空白导入 (blank import)，不能通过名称引用", targetImportPath)
		}
		return refName, nil
	}

	return "", nil
}

func parseGoImports(filePath string) (GoImportInfo, error) {
	importPattern := regexp.MustCompile(`^\s*([a-zA-Z0-9_.]*)\s*"([^"]+)"`)
	importsMap := make(GoImportInfo)
	inImportBlock := false

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "import (") {
			inImportBlock = true
			continue
		}

		if inImportBlock && strings.HasPrefix(line, ")") {
			inImportBlock = false
			continue
		}

		if strings.HasPrefix(line, "import ") {
			line = strings.TrimSpace(line[len("import "):])
		}

		matches := importPattern.FindStringSubmatch(line)
		if len(matches) == 3 {
			alias := strings.TrimSpace(matches[1])
			importPath := matches[2]

			if alias != "" {
				importsMap[importPath] = alias
			} else {
				parts := strings.Split(importPath, "/")
				refName := parts[len(parts)-1]
				importsMap[importPath] = refName
			}
		}
	}

	return importsMap, scanner.Err()
}

func getImportPathForFile(pathToResolve string) (string, error) {
	resolvedPath, err := filepath.Abs(pathToResolve)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		return "", fmt.Errorf("提供的路径不存在: %s", pathToResolve)
	}

	pathStr := filepath.ToSlash(resolvedPath)

	// Handle Go module cache paths
	if strings.Contains(pathStr, "/pkg/mod/") {
		parts := strings.Split(pathStr, "/pkg/mod/")
		if len(parts) < 2 {
			return "", fmt.Errorf("解析 pkg/mod 路径 '%s' 失败", pathStr)
		}

		postModPath := parts[1]
		packageDirStr := postModPath

		// Check if it's a file and get parent directory
		if fileInfo, err := os.Stat(resolvedPath); err == nil && !fileInfo.IsDir() {
			parentPath := filepath.Dir(resolvedPath)
			parentParts := strings.Split(filepath.ToSlash(parentPath), "/pkg/mod/")
			if len(parentParts) >= 2 {
				packageDirStr = parentParts[1]
			}
		}

		// Path decoding and version removal
		decodedPath := regexp.MustCompile(`!([a-z])`).ReplaceAllStringFunc(packageDirStr, func(match string) string {
			return strings.ToUpper(match[1:])
		})

		finalImportPath := regexp.MustCompile(`(@v[0-9a-zA-Z.-]+)`).ReplaceAllString(decodedPath, "")
		return finalImportPath, nil
	}

	packageDir := resolvedPath
	if fileInfo, err := os.Stat(resolvedPath); err == nil && !fileInfo.IsDir() {
		packageDir = filepath.Dir(resolvedPath)
	}

	// Find go.mod file
	currentDir := packageDir
	var moduleRoot string

	for {
		goModPath := filepath.Join(currentDir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			moduleRoot = currentDir
			break
		}

		parent := filepath.Dir(currentDir)
		if parent == currentDir {
			return "", fmt.Errorf("无法在 '%s' 的任何父目录中找到 go.mod 文件", packageDir)
		}
		currentDir = parent
	}

	// Parse module path from go.mod
	goModFile := filepath.Join(moduleRoot, "go.mod")
	file, err := os.Open(goModFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var modulePath string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				modulePath = parts[1]
				break
			}
		}
	}

	if modulePath == "" {
		return "", fmt.Errorf("在 '%s' 文件中未找到 'module' 声明", goModFile)
	}

	// Calculate relative path
	relPath, err := filepath.Rel(moduleRoot, packageDir)
	if err != nil {
		return "", err
	}

	relPathSlash := filepath.ToSlash(relPath)
	if relPathSlash == "." {
		return modulePath, nil
	}

	return fmt.Sprintf("%s/%s", modulePath, relPathSlash), nil
}

// isRangeInside checks if inner range is inside outer range
func isRangeInside(innerRange, outerRange lsp.Range) bool {
	return comparePosition(innerRange.Start, outerRange.Start) >= 0 &&
		comparePosition(innerRange.End, outerRange.End) <= 0
}

func comparePosition(a, b lsp.Position) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Character - b.Character
}

func readSnippet(filePath string, textRange lsp.Range) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(content), "\n")
	start := textRange.Start
	end := textRange.End

	if start.Line >= len(lines) || end.Line >= len(lines) || start.Line < 0 || end.Line < 0 {
		return "", fmt.Errorf("Range is out of the file's bounds")
	}

	if start.Line == end.Line {
		line := lines[start.Line]
		if start.Character >= len(line) || end.Character > len(line) {
			return "", fmt.Errorf("Character position out of bounds")
		}
		return line[start.Character:end.Character], nil
	}

	var snippetParts []string

	// First line
	firstLine := lines[start.Line]
	if start.Character < len(firstLine) {
		snippetParts = append(snippetParts, firstLine[start.Character:])
	}

	// Middle lines
	for i := start.Line + 1; i < end.Line; i++ {
		snippetParts = append(snippetParts, lines[i])
	}

	// Last line
	lastLine := lines[end.Line]
	if end.Character <= len(lastLine) {
		snippetParts = append(snippetParts, lastLine[:end.Character])
	}

	return strings.Join(snippetParts, "\n"), nil
}

// findSymbolBySelectionRange finds a symbol that contains the target selection range
func findSymbolBySelectionRange(symbols []lsp.SymbolInformation, targetRange lsp.Range) *lsp.SymbolInformation {
	for i := range symbols {
		if isRangeInside(targetRange, symbols[i].Location.Range) {
			return &symbols[i]
		}
	}
	return nil
}

// getFullRangeOfCalledFunction gets the full range and code snippet of a called function
func (client *LSPClient) getFullRangeOfCalledFunction(sourceURI string, callPosition lsp.Position) (*lsp.Location, *lsp.Range, string, error) {
	// Read file content
	filePath := strings.TrimPrefix(sourceURI, "file://")
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to read file: %w", err)
	}

	// Open document
	if err := client.DidOpen(sourceURI, "go", string(content)); err != nil {
		return nil, nil, "", fmt.Errorf("failed to open document: %w", err)
	}

	// Get definition
	locations, err := client.GetDefinition(sourceURI, callPosition)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get definition: %w", err)
	}

	if len(locations) == 0 {
		return nil, nil, "", fmt.Errorf("no definition found")
	}

	location := &locations[0]

	// Read target file content
	targetFilePath := strings.TrimPrefix(string(location.URI), "file://")
	targetContent, err := os.ReadFile(targetFilePath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to read target file: %w", err)
	}

	// Open target document
	if err := client.DidOpen(string(location.URI), "go", string(targetContent)); err != nil {
		return nil, nil, "", fmt.Errorf("failed to open target document: %w", err)
	}

	// Get document symbols
	symbols, err := client.GetDocumentSymbols(string(location.URI))
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get document symbols: %w", err)
	}

	// Find matching symbol
	matchingSymbol := findSymbolBySelectionRange(symbols, location.Range)
	if matchingSymbol == nil {
		return location, &location.Range, "", nil
	}

	// Read code snippet
	codeSnippet, err := readSnippet(targetFilePath, matchingSymbol.Location.Range)
	if err != nil {
		return location, &matchingSymbol.Location.Range, "", fmt.Errorf("failed to read snippet: %w", err)
	}

	return location, &matchingSymbol.Location.Range, codeSnippet, nil
}

func (client *LSPClient) init() error {
	projectRoot, err := GetRootPath()
	if err != nil {
		return err
	}
	serverPath := "gopls"
	if err := client.StartGopls(serverPath); err != nil {
		return fmt.Errorf("failed to start gopls: %w", err)
	}
	rootURI := "file://" + projectRoot
	if err := client.Initialize(rootURI); err != nil {
		return fmt.Errorf("failed to initialize LSP: %w", err)
	}
	time.Sleep(100 * time.Millisecond)
	return nil
}

// getReferencedCodeSnippet gets referenced code snippet for a position
func (client *LSPClient) getReferencedCodeSnippet(fileURL string, position lsp.Position) (Dependency, error) {
	uri := "file://" + fileURL
	location, _, codeSnippet, err := client.getFullRangeOfCalledFunction(uri, position)

	result := Dependency{
		ReferencedURL: "",
		CodeSnippet:   "",
	}

	if err != nil {
		return result, nil
	}

	if location != nil {
		result.ReferencedURL = string(location.URI)
		result.CodeSnippet = codeSnippet
		// Check if we need to find import package
		referencedFile := strings.TrimPrefix(string(location.URI), "file://")
		if referencedFile != fileURL {
			importPackage, err := findGoReferenceName(fileURL, referencedFile)
			if err != nil {
				log.Printf("Error finding import package: %v", err)
			} else if importPackage != "" {
				result.RefModule = importPackage
			}
		}
	}
	return result, nil
}

func (client *LSPClient) getReferencedCodeSnippetByLSP(referencedURL string, rangeData Range) (Dependency, error) {
	content, err := os.ReadFile(referencedURL)
	if err != nil {
		return Dependency{}, err
	}

	converter := NewPositionConverter(string(content))
	// Extract range information
	line, character := rangeData.End.Line, rangeData.End.Character
	lspPosUTF16 := converter.TreeSitterToLSP(map[string]int{
		"row":    int(line),
		"column": int(character),
	})

	position := lsp.Position{
		Line:      lspPosUTF16["line"],
		Character: lspPosUTF16["character"],
	}

	refInfo, err := client.getReferencedCodeSnippet(referencedURL, position)
	if err != nil {
		return Dependency{}, err
	}
	return refInfo, nil
}


func getDeps(deps []Dependency, pred func(Dependency) bool) []Dependency {
	var result []Dependency
	for _, dep := range deps {
		if pred(dep) {
			result = append(result, dep)
		}
	}
	return result
}

func HanderDeps(item *DataSetItem) {
	// 获取当前执行路径
	execPath, err := os.Getwd()
	if err != nil {
		log.Printf("Error getting current working directory: %v", err)
		return
	}

	systemDeps := []Dependency{}
	thirdPartyDeps := []Dependency{}
	repoDeps := []Dependency{}
	for _, dep := range item.Dependencies {
		if strings.Contains(dep.ReferencedURL, "go/pkg/mod/golang.org") {
			systemDeps = append(systemDeps, dep)
		} else if strings.Contains(dep.ReferencedURL, execPath) {
			 repoDeps= append(repoDeps, dep)
		}else {
			 thirdPartyDeps = append(thirdPartyDeps, dep)
		}
	}
	item.RepoDependencies = repoDeps
	item.ThirdPartyDependencies = thirdPartyDeps
	item.SystemDependencies = systemDeps
	item.Dependencies = nil
}

func HandlerCodeDepContexts(data EvaluationResults) (EvaluationResults, error) {
	newDataset := []DataSetItem{}
	client := NewLSPClient()
	defer client.Shutdown()
	err := client.init()
	if err != nil {
		return data, err
	}
		// 获取当前执行路径
	execPath, err := os.Getwd()
	if err != nil {
		log.Printf("Error getting current working directory: %v", err)
	}
	for _, value := range data.Dataset {
		var newDependencies []Dependency
		for i, dependency := range value.Dependencies {
			fmt.Printf("  Processing dependency %d/%d\n", i+1, len(value.Dependencies))

			depInfo, err := client.getReferencedCodeSnippetByLSP(
				value.FilePath,
				dependency.Range,
			)
			if err != nil {
				newDependencies = append(newDependencies, dependency)
				continue
			}
			newDep := Dependency{
				TreeSitterRange: dependency.TreeSitterRange,
				Range:           dependency.Range,
				NodeName:        dependency.NodeName,
				ReferencedURL:   depInfo.ReferencedURL,
				CodeSnippet:     depInfo.CodeSnippet,
				RefModule:       depInfo.RefModule,
			}
			newDependencies = append(newDependencies, newDep)
		}
		value.Dependencies = newDependencies
		value.Dependencies = DepsReduceDuplicate(value.GroundTruth, value.Dependencies)
		value.FilePath = strings.ReplaceAll(value.FilePath, execPath, ".")
		HanderDeps(&value)
		value.CoverDetails = map[string]interface{}{
			"line_cover_rate":       float64(len(value.CoveredLines)) / float64(value.EndLine - value.StartLine + 1),
		}
		newDataset = append(newDataset, value)
	}

	data.Dataset = newDataset
	return data, nil
}
