package main

import (
	"bufio"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"regexp"
)

const usageDoc = `Calculate cognitive complexities of Go functions.

Usage:

  gocognit [<flag> ...] <Go file or directory> ...

Flags:

  -over N       show functions with complexity > N only
                and return exit code 1 if the output is non-empty
  -top N        show the top N most complex functions only
  -avg          show the average complexity over all functions,
                not depending on whether -over or -top are set
  -test         indicates whether test files should be included
  -json         encode the output as JSON
  -d 	        enable diagnostic output
  -f format     string the format to use 
                (default "{{.Complexity}} {{.PkgName}} {{.FuncName}} {{.Pos}}")
  -ignore expr  ignore files matching the given regexp

The (default) output fields for each line are:

  <complexity> <package> <function> <file:row:column>

The (default) output fields for each line are:

  {{.Complexity}} {{.PkgName}} {{.FuncName}} {{.Pos}}

or equal to <complexity> <package> <function> <file:row:column>

The struct being passed to the template is:

  type Stat struct {
    PkgName     string
    FuncName    string
    Complexity  int
    Pos         token.Position
    Diagnostics []Diagnostics
  }

  type Diagnostic struct {
    Inc     string
    Nesting int
    Text    string
    Pos     DiagnosticPosition
  }	

  type DiagnosticPosition struct {
    Offset int
    Line   int
    Column int
  }
`


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
	DependencyType string `json:"dependency_type"`
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
	GitRepo                string                 `json:"git_repo"`
	RepoModule             string                 `json:"repo_module"`
	BaseCommit         string                 	   `json:"base_commit"`
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
	RepoDependencies       []Dependency           `json:"repo_dependencies,omitempty"`
	ThirdPartyDependencies []Dependency           `json:"third_party_dependencies,omitempty"`
	CoveredLines           []int                  `json:"covered_lines,omitempty"`
	CoverDetails           map[string]interface{} `json:"cover_details,omitempty"`
	DifficultyScore 		float64 `json:"difficulty_score,omitempty"`
}
const (
	defaultOverFlagVal = 0
	defaultTopFlagVal  = -1
)

// const defaultFormat = "{{.Complexity}} {{.PkgName}} {{.FuncName}} {{.Pos}}"
var farkPackageHeader = `package main

// 自定义包头部内容
`

func ComplexityScore(codeSnapshot string) int {
	tempDir, err := os.MkdirTemp("", "gocognit-*")
	if err != nil {
		log.Fatalf("创建临时目录失败: %v", err)
	}
	// 延迟清理临时目录
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()
	tempFile := filepath.Join(tempDir, "code.go")
	file, err := os.Create(tempFile)
	if err != nil {
		log.Fatalf("创建临时文件失败: %v", err)
	}
	defer func() {
		_ = file.Close()
	}()
	writer := bufio.NewWriter(file)
	_, err = writer.WriteString(farkPackageHeader)
	if err != nil {
		log.Fatalf("写入包头部失败: %v", err)
	}
	_, err = writer.WriteString(codeSnapshot)
	if err != nil {
		log.Fatalf("写入代码内容失败: %v", err)
	}
	// 刷新缓冲区确保内容写入磁盘
	if err := writer.Flush(); err != nil {
		log.Fatalf("刷新文件缓冲区失败: %v", err)
	}
	return main1(tempFile)
}


func main1(fileName string) int {
	var (
		// over              int = 0
		// top               int = -1
		// avg               bool
		// includeTests      bool = true
		// format            string = defaultFormat
		// jsonEncode        bool = false
		enableDiagnostics bool = false
		// ignoreExpr        string = ""
	)
	log.SetFlags(0)
	log.SetPrefix("gocognit: ")

	// tmpl, err := template.New("gocognit").Parse(format)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	stats, err := analyzeFile(fileName, nil, enableDiagnostics)
	if err != nil {
		log.Fatal(err)
	}
	if len(stats) != 1 {
		log.Fatalf("expected 1 file, got %d", len(stats))
	}
	// sort.Sort(byComplexity(stats))
	
	// ignoreRegexp, err := prepareRegexp(ignoreExpr)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// filteredStats := filterStats(stats, ignoreRegexp, top, over)


	// return float32(average(filteredStats))
	return stats[0].Complexity
}


func analyzeFile(fname string, stats []Stat, includeDiagnostic bool) ([]Stat, error) {
	fset := token.NewFileSet()

	f, err := parser.ParseFile(fset, fname, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	return ComplexityStatsWithDiagnostic(f, fset, stats, includeDiagnostic), nil
}



func prepareRegexp(expr string) (*regexp.Regexp, error) {
	if expr == "" {
		return nil, nil
	}

	return regexp.Compile(expr)
}

func filterStats(sortedStats []Stat, ignoreRegexp *regexp.Regexp, top, over int) []Stat {
	var filtered []Stat

	i := 0
	for _, stat := range sortedStats {
		if i == top {
			break
		}

		if stat.Complexity <= over {
			break
		}

		if ignoreRegexp != nil && ignoreRegexp.MatchString(stat.Pos.Filename) {
			continue
		}

		filtered = append(filtered, stat)
		i++
	}

	return filtered
}


func average(stats []Stat) float64 {
	total := 0
	for _, s := range stats {
		total += s.Complexity
	}

	return float64(total) / float64(len(stats))
}

type byComplexity []Stat

func (s byComplexity) Len() int      { return len(s) }
func (s byComplexity) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byComplexity) Less(i, j int) bool {
	return s[i].Complexity >= s[j].Complexity
}
