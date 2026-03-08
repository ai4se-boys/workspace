package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

type TestStatus int

const (
	StatusSuccess TestStatus = iota
	StatusFailure
	StatusSkip
	StatusNoTests
)

type GoImportInfo map[string]string

type FileInfo struct {
	FilePath      string         `json:"file_path"`
	Name          string         `json:"name"`
	Lines         []int          `json:"lines"`
	FunctionInfos []FunctionInfo `json:"coverage_result"`
}

type FunctionInfo struct {
	Name         string       `json:"name"`
	Signature    string       `json:"signature"`
	FunctionBody string       `json:"function_body"`
	FunctionDoc  string       `json:"function_doc"`
	StartLine    int          `json:"start_line"`
	EndLine      int          `json:"end_line"`
	Dependencies []Dependency `json:"dependencies"`
}

type Dependency struct {
	TreeSitterRange string `json:"-"`
	Range           Range  `json:"-"`
	NodeName        string `json:"-"`
	ReferencedURL   string `json:"referenced_url"`
	CodeSnippet     string `json:"code_snippet"`
	RefModule       string `json:"ref_module"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type TestCase struct {
	Package     string               `json:"package"`
	RelFilePath string               `json:"rel_file_path"`
	FuncName    string               `json:"func_name"`
	Cover       *map[string]FileInfo `json:"cover,omitempty"`
}

type TestCaseResult struct {
	TestCase
	StdOutput string `json:"std_output"`
	Status    TestStatus
}

type DataSetItem struct {
	ID                     string                 `json:"id"`
	TestCases              []TestCase             `json:"testcases"`
	Name                   string                 `json:"name"`
	Signature              string                 `json:"signature"`
	GroundTruth            string                 `json:"ground_truth"`
	FunctionComment        string                 `json:"function_comment"`
	FunctionStatement      string                 `json:"function_statement"`
	StartLine              int                    `json:"start_line"`
	EndLine                int                    `json:"end_line"`
	FilePath               string                 `json:"file_path"`
	Dependencies           []Dependency           `json:"dependencies,omitempty"`
	RepoDependencies       []Dependency           `json:"repo_dependencies,omitempty"`
	ThirdPartyDependencies []Dependency           `json:"third_party_dependencies,omitempty"`
	SystemDependencies     []Dependency           `json:"system_dependencies,omitempty"`
	CoveredLines           []int                  `json:"covered_lines,omitempty"`
	CoverDetails           map[string]interface{} `json:"cover_details,omitempty"`
}

type CoverDetail struct {
	Line int `json:"line"`
}

type EvaluationResults struct {
	TotalCount   int           `json:"total_count"`
	ExcludeCount int           `json:"exclude_count"`
	SuccessCount int           `json:"success_count"`
	FailedCount  int           `json:"failed_count"`
	SkipCount    int           `json:"skip_count"`
	TestCases    []TestCase    `json:"test_cases"`
	RepoModule   string        `json:"repo_module"`
	BaseCommit   string        `json:"base_commit"`
	GitRepo      string        `json:"git_repo"`
	Dataset      []DataSetItem `json:"dataset"`
}

type ModuleInfo struct {
	Path string `json:"path"`
	Dir  string `json:"dir"`
}
type GoPackageInfo struct {
	Dir        string
	ImportPath string
	Module     ModuleInfo `json:"module"`
}

type CoverageConfig struct {
	ExcludeFiles []string `yaml:"exclude_files"`
	IncludeFiles []string `yaml:"include_files"`
}

type YmlConfig struct {
	CoverageConfig CoverageConfig `yaml:"coverage_config"`
}

var (
	MaxWorkers  = 30
	TestTimeout = 300
)

func CreateParser() *sitter.Parser {
	p := sitter.NewParser()
	p.SetLanguage(golang.GetLanguage())
	return p
}

func init() {
	if os.Getenv("MAX_WORKERS") != "" {
		MAX_WORKERS_STR := os.Getenv("MAX_WORKERS")
		maxWorkers, _ := strconv.Atoi(MAX_WORKERS_STR)
		MaxWorkers = maxWorkers
	}
	if os.Getenv("TEST_TIMEOUT") != "" {
		TEST_TIMEOUT_STR := os.Getenv("TEST_TIMEOUT")
		testTimeout, _ := strconv.Atoi(TEST_TIMEOUT_STR)
		TestTimeout = testTimeout
	}
}

func GetRootPath() (string, error) {
	_, b, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get caller information")
	}
	dir := filepath.Dir(b)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func GetGoModulePath() (string, error) {
	cmd := exec.Command("go", "list", "-m")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("error getting go module path: %w\n%s", err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

func GetGoPackagesInfo() ([]GoPackageInfo, error) {
	cmd := exec.Command("go", "list", "-json", "./...")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error getting Go packages: %w\n%s", err, output)
	}
	var packages []GoPackageInfo
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var pkg GoPackageInfo
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("error decoding package info: %w\n", err)
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

func runGitCommand(args ...string) (string, error) {
	cmd := exec.Command("git", args...)

	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git command failed: %s", string(exitError.Stderr))
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func GetRepoInfo() (string, string, error) {
	// 首先，检查 'git' 命令是否存在于系统中
	if _, err := exec.LookPath("git"); err != nil {
		return "", "", fmt.Errorf("系统未安装 'git' 命令或未在 PATH 中")
	}
	commitHash, err := runGitCommand("rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("获取 commit hash 失败: %w", err)
	}
	repoURL, err := runGitCommand("config", "--get", "remote.origin.url")
	if err != nil {
		repoURL = ""
	}

	return commitHash, repoURL, nil
}

func ParseCoverageFile(lines []string) map[string][]int {
	coverage := make(map[string][]int)
	coverageSet := make(map[string]map[int]bool)

	// 跳过第一行的 mode: set
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}

		fileRange := parts[0]
		count := parts[2]

		// 跳过未覆盖的代码块
		if count == "0" {
			continue
		}

		colonIndex := strings.LastIndex(fileRange, ":")
		if colonIndex == -1 {
			continue
		}

		filePath := fileRange[:colonIndex]
		lineRange := fileRange[colonIndex+1:]

		rangeParts := strings.Split(lineRange, ",")
		if len(rangeParts) != 2 {
			continue
		}

		startPos := strings.Split(rangeParts[0], ".")[0]
		endPos := strings.Split(rangeParts[1], ".")[0]

		startLine, err1 := strconv.Atoi(startPos)
		endLine, err2 := strconv.Atoi(endPos)

		if err1 != nil || err2 != nil {
			continue
		}

		if coverageSet[filePath] == nil {
			coverageSet[filePath] = make(map[int]bool)
		}

		for lineNum := startLine; lineNum <= endLine; lineNum++ {
			coverageSet[filePath][lineNum] = true
		}
	}

	// 转换为排序的切片
	for file, lineSet := range coverageSet {
		var lines []int
		for line := range lineSet {
			lines = append(lines, line)
		}
		sort.Ints(lines)
		coverage[file] = lines
	}

	return coverage
}

func FilterTestCases(allCases []TestCase, coverageConfig CoverageConfig) []TestCase {
	var filteredCases []TestCase
	if len(coverageConfig.ExcludeFiles) == 0 && len(coverageConfig.IncludeFiles) == 0 {
		return allCases
	}
	for _, tc := range allCases {
		var isExcluded, isIncluded bool
		for _, pattern := range coverageConfig.ExcludeFiles {
			if match, _ := doublestar.Match(pattern, tc.RelFilePath); match {
				isExcluded = true
				break
			}
		}
		for _, pattern := range coverageConfig.IncludeFiles {
			if match, _ := doublestar.Match(pattern, tc.RelFilePath); match {
				isIncluded = true
				break
			}
		}
		shouldKeep := (len(coverageConfig.IncludeFiles) == 0 || isIncluded) && !(isExcluded && !isIncluded)
		if shouldKeep {
			filteredCases = append(filteredCases, tc)
		}
	}
	return filteredCases
}

func FindTestFunctionsInDir(packageDir string) ([]TestCase, error) {
	var testFunctions []TestCase
	testFuncPattern := regexp.MustCompile(`^func\s+(Test\w+)\s*\(`)
	files, err := os.ReadDir(packageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("error reading directory %s: %w", packageDir, err)
	}
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), "_test.go") {
			filePath := filepath.Join(packageDir, file.Name())
			f, err := os.Open(filePath)
			if err != nil {
				log.Printf("Warning: Error opening %s: %v", filePath, err)
				continue
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				if match := testFuncPattern.FindStringSubmatch(scanner.Text()); len(match) > 1 {
					testFunctions = append(testFunctions, TestCase{RelFilePath: file.Name(), FuncName: match[1]})
				}
			}
			if err := scanner.Err(); err != nil {
				log.Printf("Warning: Error reading %s: %v", filePath, err)
			}
			f.Close()
		}
	}
	return testFunctions, nil
}

func FindTests(ymlConfig *YmlConfig, rootPath string) (EvaluationResults, error) {
	log.Println("Starting to find all Go test functions in the repository...")
	modulePath, err := GetGoModulePath()
	if err != nil {
		log.Fatalf("Fatal: Could not determine Go module path: %v", err)
		return EvaluationResults{}, err
	}
	log.Printf("Detected module path: %s", modulePath)
	packages, err := GetGoPackagesInfo()
	if err != nil {
		log.Fatalf("Fatal: Could not list Go packages: %v", err)
		return EvaluationResults{}, err
	}
	log.Printf("Found %d packages. Analyzing each for test functions...", len(packages))

	var allTests []TestCase
	for i, pkg := range packages {
		if strings.Contains(pkg.ImportPath, "/vendor/") {
			continue
		}
		fmt.Printf("[%d/%d] Processing package: %s\n", i+1, len(packages), pkg.ImportPath)

		// Use absolute path for pkg.Dir to avoid issues when running from subdir
		absPkgDir := pkg.Dir
		if !filepath.IsAbs(absPkgDir) {
			absPkgDir = filepath.Join(rootPath, absPkgDir)
		}

		testsInPkg, err := FindTestFunctionsInDir(absPkgDir)
		if err != nil {
			log.Printf("Warning: Could not process package %s: %v", pkg.ImportPath, err)
			continue
		}

		for _, testInfo := range testsInPkg {
			testInfo.Package = pkg.ImportPath
			fullPath := filepath.Join(pkg.ImportPath, testInfo.RelFilePath)
			relativePath := strings.TrimPrefix(fullPath, modulePath+"/")
			testInfo.RelFilePath = relativePath
			allTests = append(allTests, testInfo)
		}
	}

	log.Println("\n--- Scan Complete ---")
	log.Printf("Total test functions found: %d", len(allTests))

	totalCnt := len(allTests)
	filteredTests := FilterTestCases(allTests, ymlConfig.CoverageConfig)
	excludeCnt := totalCnt - len(filteredTests)

	report := EvaluationResults{
		TotalCount:   totalCnt,
		ExcludeCount: excludeCnt,
		TestCases:    filteredTests,
		RepoModule:   modulePath,
	}
	return report, nil
}

func RunTests(inputData EvaluationResults, SuccessOutputFile, FailedOutputFile string) EvaluationResults {
	testsToRun := inputData.TestCases
	if len(testsToRun) == 0 {
		log.Println("No test cases to run.")
		return inputData
	}

	log.Printf("Starting test run for %d test cases with %d concurrent workers.", len(testsToRun), MaxWorkers)

	jobs := make(chan TestCase, len(testsToRun))
	results := make(chan TestCaseResult, len(testsToRun))

	var wg sync.WaitGroup
	// Start workers
	for i := 0; i < MaxWorkers; i++ {
		wg.Add(1)
		go worker(&wg, i+1, inputData.RepoModule, jobs, results)
	}

	// Send jobs
	for _, tc := range testsToRun {
		jobs <- tc
	}
	close(jobs)

	// Wait for all workers to finish
	wg.Wait()
	close(results)

	var successfulTests []TestCase
	var failedCount, skipCount int
	if err := os.Remove(FailedOutputFile); err != nil {
		log.Printf("Warning: failed to remove old file %s: %v", FailedOutputFile, err)
	}
	failedLogFile, err := os.OpenFile(FailedOutputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Warning: Cannot open failed log file: %v", err)
	}
	defer failedLogFile.Close()
	failedEncoder := json.NewEncoder(failedLogFile)
	for result := range results {
		switch result.Status {
		case StatusSuccess:
			successfulTests = append(successfulTests, result.TestCase)
		case StatusFailure:
			failedCount++
			failedTest := TestCaseResult{
				TestCase:  result.TestCase,
				StdOutput: result.StdOutput,
				Status:    StatusFailure,
			}
			if err := failedEncoder.Encode(failedTest); err != nil {
				log.Printf("Warning: failed to write to failure log: %v", err)
			}
		case StatusSkip:
			skipCount++
		}
	}

	log.Println("--- Test Run Complete ---")

	finalReport := EvaluationResults{
		RepoModule:   inputData.RepoModule,
		BaseCommit:   inputData.BaseCommit,
		GitRepo:      inputData.GitRepo,
		TotalCount:   inputData.TotalCount,
		ExcludeCount: inputData.ExcludeCount,
		SuccessCount: len(successfulTests),
		FailedCount:  failedCount,
		SkipCount:    skipCount,
		TestCases:    successfulTests,
	}
	return finalReport
}

func nodeToRange(node *sitter.Node) Dependency {
	jsonData, _ := json.Marshal(node.Range())
	return Dependency{
		TreeSitterRange: string(jsonData),
		NodeName:        node.String(),
		Range: Range{
			Start: Position{
				Line:      int(node.StartPoint().Row),
				Character: int(node.StartPoint().Column),
			},
			End: Position{
				Line:      int(node.EndPoint().Row),
				Character: int(node.EndPoint().Column),
			},
		},
	}
}

// 获取所有节点
func getAllNodes(node *sitter.Node) []*sitter.Node {
	var nodes []*sitter.Node

	var traverse func(*sitter.Node)
	traverse = func(n *sitter.Node) {
		nodes = append(nodes, n)
		for i := 0; i < int(n.ChildCount()); i++ {
			traverse(n.Child(i))
		}
	}
	traverse(node)
	return nodes
}

func getFunctionSignature(node *sitter.Node, source []byte) string {
	var signatureParts []string

	// 处理方法接收者
	if node.Type() == "method_declaration" {
		receiver := node.ChildByFieldName("receiver")
		if receiver != nil {
			receiverText := strings.TrimSpace(string(source[receiver.StartByte():receiver.EndByte()]))
			signatureParts = append(signatureParts, receiverText)
		}
	}

	// 添加函数名
	name := node.ChildByFieldName("name")
	if name != nil {
		signatureParts = append(signatureParts, string(source[name.StartByte():name.EndByte()]))
	}

	// 添加参数列表
	params := node.ChildByFieldName("parameters")
	if params != nil {
		paramsText := strings.TrimSpace(string(source[params.StartByte():params.EndByte()]))
		signatureParts = append(signatureParts, paramsText)
	}

	// 添加返回类型
	result := node.ChildByFieldName("result")
	if result != nil {
		resultText := strings.TrimSpace(string(source[result.StartByte():result.EndByte()]))
		signatureParts = append(signatureParts, resultText)
	}

	return strings.Join(signatureParts, " ")
}
func getFunctionComment(funcNode *sitter.Node, content []byte, root *sitter.Node) string {
	var comments []string

	// 获取函数开始位置的行号
	funcStartRow := funcNode.StartPoint().Row

	// 向前查找注释，找到函数前面的注释块
	var commentNodes []*sitter.Node

	// 遍历根节点的所有子节点，找到注释
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "comment" {
			// 检查注释是否在函数之前且相邻
			commentEndRow := child.EndPoint().Row
			if commentEndRow < funcStartRow {
				commentNodes = append(commentNodes, child)
			}
		}
	}

	// 过滤出紧邻函数的注释（中间没有空行或其他代码）
	var relevantComments []*sitter.Node
	if len(commentNodes) > 0 {
		// 从最后一个注释开始检查（最接近函数的注释）
		for i := len(commentNodes) - 1; i >= 0; i-- {
			comment := commentNodes[i]
			commentEndRow := comment.EndPoint().Row

			// 检查注释和函数之间是否有其他非空白内容
			if i == len(commentNodes)-1 {
				// 最后一个注释，检查它和函数之间的距离
				if funcStartRow-commentEndRow <= 2 { // 允许1-2行的间隔
					relevantComments = append([]*sitter.Node{comment}, relevantComments...)
				}
			} else {
				// 检查当前注释和下一个注释之间是否连续
				nextComment := commentNodes[i+1]
				nextCommentStartRow := nextComment.StartPoint().Row
				if nextCommentStartRow-commentEndRow <= 2 {
					relevantComments = append([]*sitter.Node{comment}, relevantComments...)
				} else {
					break // 如果不连续，停止向前查找
				}
			}
		}
	}

	// 提取注释文本
	for _, commentNode := range relevantComments {
		commentText := string(content[commentNode.StartByte():commentNode.EndByte()])
		comments = append(comments, commentText)
	}

	if len(comments) > 0 {
		return strings.Join(comments, "\n")
	}

	return "" // 如果没有找到注释，返回空字符串
}

// // 识别函数范围
func identifyFunctionRanges(goFilePath string) (map[int]FunctionInfo, error) {
	bytes, err := os.Open(goFilePath)
	if err != nil {
		return nil, nil
	}
	content, err := io.ReadAll(bytes)
	if err != nil {
		return nil, err
	}
	// Note: parser 不是线程安全的
	parser := CreateParser()
	tree := parser.Parse(nil, content)
	root := tree.RootNode()

	lineToFunction := make(map[int]FunctionInfo)

	var traverse func(*sitter.Node, []string)
	traverse = func(node *sitter.Node, parentTypes []string) {
		currentParentTypes := make([]string, len(parentTypes))
		copy(currentParentTypes, parentTypes)

		if node.Parent() != nil {
			currentParentTypes = append(currentParentTypes, node.Parent().Type())
		}

		// 检查是否为最外层函数/方法
		if node.Type() == "function_declaration" || node.Type() == "method_declaration" {
			// 检查是否为包级别声明
			allowedParents := map[string]bool{
				"source_file":         true,
				"package_declaration": true,
				"import_declaration":  true,
				"declaration_list":    true,
			}

			isTopLevel := true
			for _, parentType := range currentParentTypes {
				if !allowedParents[parentType] && parentType != "" {
					isTopLevel = false
					break
				}
			}

			if isTopLevel {
				funcName := "unknown"
				nameNode := node.ChildByFieldName("name")
				if nameNode != nil {
					funcName = string(content[nameNode.StartByte():nameNode.EndByte()])
				}

				startLine := int(node.StartPoint().Row) + 1
				endLine := int(node.EndPoint().Row) + 1
				signature := getFunctionSignature(node, content)
				comment := getFunctionComment(node, content, root)
				body := string(content[node.StartByte():node.EndByte()])

				// 获取依赖节点
				allNodes := getAllNodes(node)
				dependencies := []Dependency{}
				for _, dep := range allNodes {
					nodeInfo := nodeToRange(dep)
					dependencies = append(dependencies, Dependency{
						TreeSitterRange: nodeInfo.TreeSitterRange,
						Range:           nodeInfo.Range,
					})
				}

				funcInfo := FunctionInfo{
					Name:         funcName,
					Signature:    signature,
					FunctionBody: body,
					FunctionDoc:  comment,
					StartLine:    startLine,
					EndLine:      endLine,
					Dependencies: dependencies,
				}

				// 为函数范围内的每一行添加映射
				for lineNum := startLine; lineNum <= endLine; lineNum++ {
					lineToFunction[lineNum] = funcInfo
				}
			}
		}

		// 递归遍历子节点
		for i := 0; i < int(node.ChildCount()); i++ {
			traverse(node.Child(i), currentParentTypes)
		}
	}

	traverse(root, nil)
	return lineToFunction, nil
}

func getFunctionForLine(goFilePath string, targetLine int) (FunctionInfo, error) {
	functionRanges, err := identifyFunctionRanges(goFilePath)
	if err != nil {
		return FunctionInfo{}, err
	}

	if funcInfo, exists := functionRanges[targetLine]; exists {
		return funcInfo, nil
	}

	return FunctionInfo{}, fmt.Errorf("no function found for line %d", targetLine)
}

func worker(wg *sync.WaitGroup, id int, repoModule string, jobs <-chan TestCase, results chan<- TestCaseResult) {
	defer wg.Done()
	for tc := range jobs {
		log.Printf("[Worker %d] Running test: %s in %s", id, tc.FuncName, tc.Package)

		// Set a timeout for the command context
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(TestTimeout)*time.Second)
		defer cancel()
		tempDir, err := os.CreateTemp("", "test-coverage-*.set")
		if err != nil {
			log.Printf("[Worker %d] FAILED test: %s with error: %v", id, tc.FuncName, err)
			results <- TestCaseResult{TestCase: tc, Status: StatusFailure, StdOutput: "" + "\nError: " + err.Error()}
			continue
		}
		defer os.Remove(tempDir.Name())
		cmd := exec.CommandContext(ctx,
			"go", "test",
			"-gcflags", "all=-N -l",
			"-run", fmt.Sprintf("^%s$", tc.FuncName),
			tc.Package,
			"-v",
			"-count=1",
			"-covermode=set",
			fmt.Sprintf("-coverprofile=%s", tempDir.Name()),
		)
		cmd.Dir = "."
		output, err := cmd.CombinedOutput()
		fmt.Println("cmd", cmd.String())
		coverageBytes, err := os.ReadFile(tempDir.Name())
		if err != nil {
			log.Printf("[Worker %d] continue test: %s with error: %v", id, tc.FuncName, err)
			continue
		}
		coverFiles := ParseCoverageFile(strings.Split(string(coverageBytes), "\n"))
		FileInfoMap := make(map[string]FileInfo)
		for coverFile, coverLines := range coverFiles {
			realPath := strings.Replace(coverFile, repoModule, ".", 1)
			absPath, _ := filepath.Abs(realPath)
			var FunctionInfos []FunctionInfo
			functionNamesSet := make(map[string]bool)
			for _, line := range coverLines {
				functionInfo, err := getFunctionForLine(absPath, line)
				if err != nil {
					fmt.Printf("line %d not found function_info: %v\n", line, err)
					continue
				}

				if functionInfo.Name != "" && !functionNamesSet[functionInfo.Name] {
					FunctionInfos = append(FunctionInfos, functionInfo)
					functionNamesSet[functionInfo.Name] = true
				}
			}
			relPath := strings.Replace(coverFile, repoModule, ".", 1)
			refFile, _ := filepath.Abs(relPath)
			FileInfoMap[coverFile] = FileInfo{
				Name:          coverFile,
				Lines:         coverLines,
				FilePath:      refFile,
				FunctionInfos: FunctionInfos,
			}
		}
		tc.Cover = &FileInfoMap

		if err != nil {
			log.Printf("[Worker %d] FAILED test: %s with error: %v", id, tc.FuncName, err)
			results <- TestCaseResult{TestCase: tc, Status: StatusFailure, StdOutput: string(output) + "\nError: " + err.Error()}
			continue
		}
		outputStr := string(output)

		// Check for context deadline exceeded
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[Worker %d] TIMEOUT for test: %s", id, tc.FuncName)
			results <- TestCaseResult{TestCase: tc, Status: StatusFailure, StdOutput: outputStr + "\nError: Test timed out."}
			continue
		}
		passStr := fmt.Sprintf("--- PASS: %s", tc.FuncName)
		result := TestCaseResult{
			TestCase:  tc,
			StdOutput: outputStr,
		}
		if err == nil && strings.Contains(outputStr, passStr) {
			result.Status = StatusSuccess
			log.Printf("[Worker %d] SUCCESS for test: %s", id, tc.FuncName)
		} else {
			if strings.Contains(outputStr, "--- SKIP:") {
				result.Status = StatusSkip
				log.Printf("[Worker %d] SKIPPED test: %s", id, tc.FuncName)
			} else if strings.Contains(outputStr, "[no tests to run]") {
				result.Status = StatusSkip
				log.Printf("[Worker %d] NO TESTS found for: %s", id, tc.FuncName)
			} else {
				result.Status = StatusFailure
				log.Printf("[Worker %d] FAILED test: %s", id, tc.FuncName)
			}
		}
		results <- result
	}
}

func WriteJSON(filename string, data interface{}) {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("Fatal: Failed to marshal JSON for %s: %v", filename, err)
	}
	if err = os.WriteFile(filename, jsonData, 0644); err != nil {
		log.Fatalf("Fatal: Failed to write to file %s: %v", filename, err)
	}
}

func HandlerTestCoverDetails(rootPath string, successOutputFile, failedOutputFile string) EvaluationResults {
	report, err := FindTests(&YmlConfig{}, rootPath)
	if err != nil {
		log.Fatalf("Fatal: Failed to find tests: %v", err)
	}
	baseCommit, gitRepo, err := GetRepoInfo()
	if err != nil {
		log.Printf("Warning: Could not get repo info: %v", err)
	}
	report.BaseCommit = baseCommit
	report.GitRepo = gitRepo
	finalReport := RunTests(report, successOutputFile, failedOutputFile)
	log.Printf("Final report written to %s", successOutputFile)
	log.Printf("Summary: Total=%d, Success=%d, Failed=%d, Skipped=%d",
		len(finalReport.TestCases), finalReport.SuccessCount, finalReport.FailedCount, finalReport.SkipCount)
	return finalReport
}

func Transposition(report EvaluationResults) EvaluationResults {
	results := report.TestCases
	datasetMap := map[string]DataSetItem{}
	for _, result := range results {
		cover := result.Cover
		if cover == nil {
			continue
		}

		for coverFile, fileInfo := range *cover {
			functionInfos := fileInfo.FunctionInfos
			cover_file_path := fileInfo.FilePath
			lines := fileInfo.Lines
			for _, functionInfo := range functionInfos {
				key := coverFile + ":" + functionInfo.Name
				value, ok := datasetMap[key]
				testCases := []TestCase{}
				coverLines := []int{}
				if ok {
					testCases = value.TestCases
					coverLines = value.CoveredLines
				}
				testCases = append(testCases, result)
				coverLines = append(coverLines, lines...)
				set := make(map[int]bool)
				for _, line := range coverLines {
					set[line] = true
				}
				coverLines = []int{}
				for line := range set {
					if line >= functionInfo.StartLine && line <= functionInfo.EndLine {
						coverLines = append(coverLines, line)
					}
				}
				sort.Ints(coverLines)
				datasetMap[key] = DataSetItem{
					ID:                key,
					TestCases:         testCases,
					Name:              functionInfo.Name,
					Signature:         functionInfo.Signature,
					GroundTruth:       functionInfo.FunctionBody,
					FunctionComment:   functionInfo.FunctionDoc,
					FunctionStatement: functionInfo.FunctionDoc,
					StartLine:         functionInfo.StartLine,
					EndLine:           functionInfo.EndLine,
					FilePath:          cover_file_path,
					Dependencies:      functionInfo.Dependencies,
					CoveredLines:      coverLines,
				}
			}
		}
	}
	dataset := []DataSetItem{}
	for _, value := range datasetMap {
		newTestCase := []TestCase{}
		for _, testCase := range value.TestCases {
			newTestCase = append(newTestCase, TestCase{
				Package:     testCase.Package,
				RelFilePath: testCase.RelFilePath,
				FuncName:    testCase.FuncName,
			})
		}
		value.TestCases = newTestCase
		dataset = append(dataset, value)
	}
	report.Dataset = dataset
	return report
}

func DepsReduceDuplicate(functionBody string, deps []Dependency) []Dependency {
	depSet := make(map[string]bool)
	var newDeps []Dependency

	for _, dep := range deps {
		depData := map[string]string{
			"referenced_url": dep.ReferencedURL,
			"code_snippet":   dep.CodeSnippet,
		}
		depStr, _ := json.Marshal(depData)
		depKey := string(depStr)

		if !depSet[depKey] && dep.ReferencedURL != "" && dep.CodeSnippet != "" {
			newDeps = append(newDeps, dep)
			depSet[depKey] = true
		}
	}

	var resDeps []Dependency
	for _, dep := range newDeps {
		if !strings.Contains(dep.CodeSnippet, functionBody) {
			resDeps = append(resDeps, dep)
		}
	}

	return resDeps
}

func IsEasyCase(item DataSetItem) bool {
	// 针对 ground_truth 太短的进行过滤
	groundTruth := item.GroundTruth
	groundTruthLen := len(strings.Split(groundTruth, "\n"))
	if groundTruthLen < 10 {
		return true
	}
	// 针对没有第三方仓库依赖的进行过滤
	if len(item.ThirdPartyDependencies) < 1 {
		return true
	}
	return false
}

const DatasetOutputDir = "./data"

func main() {
	rootPath, err := GetRootPath()
	if err != nil {
		log.Fatalf("Fatal: Failed to get root path: %v", err)
	}
	os.MkdirAll(DatasetOutputDir, 0755)
	log.Printf("Dataset dir: %s", DatasetOutputDir)
	successOutputFile := filepath.Join(DatasetOutputDir, "successful_test_functions.json")
	failedOutputFile := filepath.Join(DatasetOutputDir, "failed_test_functions.jsonl")
	report := HandlerTestCoverDetails(rootPath, successOutputFile, failedOutputFile)
	WriteJSON(successOutputFile, report)
	log.Printf("Transposition dataset")
	if err != nil {
		log.Fatalf("Fatal: Failed to handler code dep contexts: %v", err)
	}
	originDatasetOutputFile := filepath.Join(DatasetOutputDir, "origin_dataset.json")
	WriteJSON(originDatasetOutputFile, report)
	report = Transposition(report)
	WriteJSON(originDatasetOutputFile, report)
	log.Printf("Add code dep contexts")
	report.TestCases = nil
	datasetOutputFile := filepath.Join(DatasetOutputDir, "dataset.json")
	WriteJSON(datasetOutputFile, report)
	log.Printf("%s has writed\n", datasetOutputFile)
	newDataset := []DataSetItem{}
	for _, item := range report.Dataset {
		if item.EndLine - item.StartLine < 10 {
			continue
		}
		if float64(len(item.CoveredLines)) / float64(item.EndLine - item.StartLine + 1) < 0.8 {
			continue
		}
		newDataset = append(newDataset, item)
	}
	report.Dataset = newDataset
	report, err = HandlerCodeDepContexts(report)
	if err != nil {
		log.Fatalf("Fatal: Failed to handler code dep contexts: %v", err)
	}
	WriteJSON(datasetOutputFile, report)
	log.Printf("%s has writed\n", datasetOutputFile)

	// analyzer := NewGoCoverageAnalyzer()
	filteredCase := []DataSetItem{}
	for _, item := range report.Dataset {
			if IsEasyCase(item) {
			continue
		}
		filteredCase = append(filteredCase, item)
	}
	report.Dataset = filteredCase
	filteredDatasetOutputFile := filepath.Join(DatasetOutputDir, "filter_dataset.json")
	WriteJSON(filteredDatasetOutputFile, report)
	log.Printf("%s has writed\n", filteredDatasetOutputFile)
}
