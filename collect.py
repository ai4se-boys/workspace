import os
import json


def handler_repo_dep(deps ):
    new_deps = []
    for dep in deps:
        new_deps.append({
            "referenced_url": dep["referenced_url"].replace("file:///Users/bytedance/code/workspace/swebench/workspace/", "").replace("file:///data00/home/baixuran/code/workspace/workspace/", ""),
            "code_snippet": dep['code_snippet'],
            "ref_module": dep['ref_module'],
        })
    return new_deps


def handler_third_dep(deps):
    new_deps = []
    for dep in deps:
        new_deps.append({
            "referenced_url": dep["referenced_url"].replace("file:///Users/bytedance/go/pkg/mod/", "").replace("file:///home/baixuran/.gvm/pkgsets/go1.22/global",""),
            "code_snippet": dep['code_snippet'],
            "ref_module": dep['ref_module'],
        })
    return new_deps



def collect():
    datas = []
    for item in os.listdir("./swebench/workspace"):
        file_path = f"./swebench/workspace/{item}/data/filter_dataset.json"
        if not os.path.exists(file_path):
            continue
        data = json.load(open(file_path))
        dataset = data["dataset"]
        git_repo = data["git_repo"]
        repo_module = data["repo_module"]
        base_commit = data["base_commit"]
        for item in dataset:
            if not item["function_comment"]:
                continue

            if "system_dependencies" in item:
                del item["system_dependencies"]

            if "repo_dependencies" in item:
                item["repo_dependencies"] = handler_repo_dep(item["repo_dependencies"])

            if "third_party_dependencies" in item:
                item["third_party_dependencies"] = handler_third_dep(
                    item["third_party_dependencies"])

            datas.append({
                "git_repo": git_repo,
                "repo_module": repo_module,
                "base_commit": base_commit,
                **item,
            })
    json.dump(datas, open("dataset.json", "w"), indent=2)


def main():
    collect()

if __name__ == "__main__":
    main()