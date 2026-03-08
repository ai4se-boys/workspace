package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
    // 读取 dataset.json
	datasetFilePath:= "/home/baixuran/code/workspace/dataset.json"
	datasetContent, err := os.ReadFile(datasetFilePath)
	if err != nil {
		println("Error reading dataset.json:", err.Error())
		return
	}
	data := []DataSetItem{}
	err = json.Unmarshal(datasetContent, &data)
	if err != nil {
		println("Error unmarshalling dataset.json:", err.Error())
		return
	}
	fmt.Println(len(data))

	newData := []DataSetItem{}
	
	for _, item := range data {
		// deps := analyzeCodeSnapshotDiagnostic(item.GroundTruth)
		deps := []Dependency{}
		for _, _ = range item.RepoDependencies {
			deps = append(deps, Dependency{
				DependencyType: InhouseDependency,
			})
		}
		for _, _ = range item.ThirdPartyDependencies {
			deps = append(deps, Dependency{
				DependencyType: ExternalDependency,
			})
		}
		calculateDifficultyScore := CalculateDifficultyScore(deps, item.GroundTruth)
		fmt.Println(calculateDifficultyScore)
		item.DifficultyScore = calculateDifficultyScore
		newData = append(newData, item)
	}
	jsonData, err := json.MarshalIndent(newData, "", "  ")
	if err != nil {
		println("Error marshalling dataset.json:", err.Error())
		return
	}
	err = os.WriteFile("./dataset_handlered.json", jsonData, 0644)
	if err != nil {
		println("Error writing dataset.json:", err.Error())
		return
	}	
}

const (
	InhouseDependency = "inhouse"
	ExternalDependency = "external"
)


// 调用内部代码库为 1 分
// 调用外部代码库为 2 分


func CalculateDifficultyScore(deps []Dependency, code_snapshot string) float64 {
	total_difficulty_score := 0.0
	for _, dep := range deps {
		if dep.DependencyType == InhouseDependency {
			total_difficulty_score += 1
		} else if dep.DependencyType == ExternalDependency {
			total_difficulty_score += 2
		} else {
			println("Unknown dependency type")
		}
	}
	
	// gocognt := countGoCodeLines(code_snapshot)
	// total_difficulty_score += float64(gocognt) * 0.01
	complexity_score := ComplexityScore(code_snapshot)
	total_difficulty_score += float64(complexity_score)
	return total_difficulty_score
}


func analyzeCodeSnapshotDiagnostic(code_snapshot string) []Dependency {
	return []Dependency{
		{
			DependencyType: InhouseDependency,
		},
		{
			DependencyType: ExternalDependency,
		},
	}
}
