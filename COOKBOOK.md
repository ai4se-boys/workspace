# Cookbook: 基于真实 Go 仓库的函数级代码生成评测

## 1. 文档目标

这份 cookbook 结合飞书方案文档中的设计思路，以及当前仓库已经存在的脚本和数据产物，整理出一条可以落地执行的函数级代码生成评测流程。

目标不是重新定义一套抽象方案，而是回答下面几个问题：

- 这个仓库现在到底能做什么。
- 各个脚本在整条链路里分别负责什么。
- 应该按什么顺序运行。
- 每一步的输入、输出和中间产物是什么。
- 当前仓库还存在哪些需要人工衔接的地方。

从定位上看，这个仓库是在做一类介于 HumanEval 和 SWE-bench 之间的评测：

- 目标粒度不是整仓库修复，而是函数级代码生成。
- 样本来自真实 Go 仓库，而不是手写 toy problem。
- 每条样本保留测试用例、函数签名、函数实现、依赖代码、函数描述和难度信息。
- 评测正确性不仅依赖生成代码，还依赖目标仓库里的真实测试。

## 2. 方案和仓库的映射关系

飞书方案的核心链路可以概括为五步：

1. 收集仓库中的测试用例。
2. 通过覆盖率把测试用例反向映射到被测函数。
3. 为函数补齐签名、依赖、逻辑描述、仓库元信息。
4. 根据覆盖率和复杂度过滤样本。
5. 让模型生成代码，再回放到原仓库里执行测试验证。

当前仓库已经把这五步拆成了若干脚本：

| 方案阶段 | 当前仓库实现 |
| --- | --- |
| 目标仓库遍历和插桩 | `execute.py` + `run.sh` + `gosrc/main/main.go` + `gosrc/main/lsp.go` |
| 函数样本聚合 | `collect.py` |
| 文本清洗/描述补充 | `filter.py` + `generate_doc.py` |
| 难度评估 | `calculate_difficulty_score/main/main.go` + `calculate_difficulty_score/main/calculate_difficulty_score.go` |
| 基准数据整理 | `swebench/raw_dataset_process.py` |
| LLM 代码生成 | `swebench/llm_generate.py` |
| 回仓验证 | `swebench/verify.py` + `run_verify.sh` |

这意味着：

- 仓库已经覆盖了采集、聚合、补充描述、难度打分、生成和验证这几类核心动作。
- 但它还不是一个统一的一键式 pipeline。
- 多个阶段之间通过 JSON 文件衔接，运行时需要手动指定或切换输入文件。

## 3. 仓库结构速览

### 3.1 根目录脚本

- `execute.py`：批量克隆外部 Go 仓库，把 Go 采集器复制到目标仓库中执行。
- `collect.py`：从 `./swebench/workspace/*/data/filter_dataset.json` 聚合生成根目录 `dataset.json`。
- `filter.py`：从 `after_dataset.json` 中提取第一个 Python 代码块，输出 `after1_dataset.json`。
- `generate_doc.py`：对 `after1_dataset.json` 中的 `ground_truth` 调用 LLM 生成伪代码描述，输出 `after2_dataset.json`。
- `run.sh`：安装适配 Go 版本的 `gopls`，然后运行 `execute.py`。
- `run_verify.sh`：调用 SWE-bench harness 进行验证。
- `data-trace.py`：对 `after_dataset.json` 做简单数据统计。

### 3.2 Go 采集器

- `gosrc/main/main.go`：核心采集程序，负责扫描 Go 仓库、执行测试、解析覆盖率、抽取函数及依赖信息，并产出数据集。
- `gosrc/main/lsp.go`：封装 `gopls` 客户端，用于基于 LSP 获取 definition、symbol 等信息，配合 tree-sitter 做依赖解析。

### 3.3 难度评估模块

- `calculate_difficulty_score/main/main.go`：读取根目录 `dataset.json`，计算每条样本的 `difficulty_score`，输出 `dataset_handlered.json`。
- `calculate_difficulty_score/main/calculate_difficulty_score.go`：难度分数的核心逻辑，综合依赖类型和认知复杂度。
- `calculate_difficulty_score/difficulty_score_analyze.py`：对 `dataset_handlered.json` 进行分层统计，并输出 easy/hard 子集。

### 3.4 swebench 子目录

- `swebench/raw_dataset_process.py`：把原始数据集转换成统一的 benchmark 输入格式。
- `swebench/analyze_code_cognitive_complexity.py`：对 Go 函数体做认知复杂度分析。
- `swebench/llm_generate.py`：按 prompt 模板为样本生成代码。
- `swebench/verify.py`：把模型生成代码回填到原仓库，再执行真实 Go 测试做验证。
- `swebench/dataset/`：存放 benchmark 数据、prompt 模板、模型输出和统计结果。

## 4. 数据模型

当前仓库围绕一个函数级样本展开。根目录 `dataset.json` 和 `swebench/dataset/go.json` 中都能看到这一设计。

一个典型样本包含：

- `git_repo`：样本所在仓库。
- `repo_module`：Go module 名称。
- `base_commit`：采样时的提交版本。
- `id`：函数唯一标识，通常包含仓库模块、文件和函数名。
- `testcases`：覆盖该函数的测试用例列表。
- `name`：函数名。
- `signature`：函数签名。
- `ground_truth`：目标函数原始实现。
- `function_comment`：简短描述或注释级描述。
- `function_statement`：更完整的功能说明。
- `file_path`、`start_line`、`end_line`：函数在仓库中的定位信息。
- `repo_dependencies`：仓库内依赖代码片段。
- `third_party_dependencies`：三方依赖代码片段。
- `difficulty_score`：难度分数。

这和飞书方案里的目标字段基本一致，只是当前仓库把描述字段命名为 `function_comment`、`function_statement` 等实际可运行字段。

## 5. 环境准备

### 5.1 基础依赖

建议在 Linux 环境下准备以下依赖：

- Git
- Go
- Python 3.10+
- `gopls`
- `pytest` 和 `pytest-cov`
- Python 包：`openai`、`pandas`、`matplotlib`、`jinja2`

可以按下面的方式准备一个最小环境：

```bash
python -m venv .venv
source .venv/bin/activate
pip install openai pandas matplotlib jinja2 pytest pytest-cov
```

Go 侧至少要保证 `go` 命令可用。`run.sh` 会尝试根据本机 Go 版本安装匹配版本的 `gopls`。

### 5.2 模型访问配置

当前仓库里多个脚本直接在源码中初始化 `AsyncAzureOpenAI` 客户端：

- `generate_doc.py`
- `swebench/llm_generate.py`
- `swebench/verify.py`

实际运行前，建议你先检查这些脚本中的模型名、endpoint 和认证信息是否与当前环境匹配。当前实现偏实验性质，没有统一的环境变量加载层。

### 5.3 目录约定

整条链路默认使用下面几个目录：

- 根目录：中间聚合产物和分析脚本。
- `./workspace`：`execute.py` 克隆目标仓库时使用的工作目录。
- `./swebench/workspace`：`collect.py` 默认读取各仓库 `filter_dataset.json` 的位置。
- `./swebench/dataset`：benchmark 数据和模型输出目录。

运行前最好先统一你自己的目录布局，否则需要改脚本中的硬编码路径。

## 6. 端到端推荐流程

下面给出一条适配当前仓库的推荐执行顺序。

### Step 1: 采集真实 Go 仓库中的函数级样本

入口：`run.sh` 或 `execute.py`

#### 作用

这一步的目标是批量处理外部 Go 仓库，得到每个仓库的函数级样本数据。

`execute.py` 会做以下事情：

1. 克隆 `repositories` 列表中的目标仓库到 `./workspace`。
2. 在每个仓库里创建 `generate/` 目录。
3. 把 `gosrc/main/main.go` 和 `gosrc/main/lsp.go` 复制进去。
4. 在目标仓库执行 `go mod tidy`。
5. 执行 `go run ./generate/*.go`。
6. 如配置允许，再提交并推送改动。

#### 运行方式

```bash
bash run.sh
```

或者直接：

```bash
python execute.py
```

#### 运行前必须确认的配置

- `execute.py` 里的 `repositories` 列表是否是你要处理的仓库。
- `source_files` 是否仍然指向当前仓库中的 Go 采集器文件。
- 你是否真的希望执行 `push_changes()`。如果只是本地采样，建议先注释推送动作，避免误推远端。

#### 预期产物

每个目标仓库执行完成后，应在该仓库自己的数据目录中生成函数级样本，后续再被汇总到：

- `./swebench/workspace/<repo>/data/filter_dataset.json`

这里要注意，根目录 `collect.py` 假定输入文件就在上面的路径下。如果实际 Go 采集器输出位置不同，需要先做目录对齐。

### Step 2: 聚合所有仓库样本

入口：`collect.py`

#### 作用

这一步把每个仓库单独产出的 `filter_dataset.json` 聚合成一个总数据集，并顺手做几件清洗工作：

- 删除 `system_dependencies`
- 规范化 `repo_dependencies.referenced_url`
- 规范化 `third_party_dependencies.referenced_url`
- 合并仓库级元信息 `git_repo`、`repo_module`、`base_commit`

#### 运行方式

```bash
python collect.py
```

#### 输入

- `./swebench/workspace/*/data/filter_dataset.json`

#### 输出

- `./dataset.json`

`dataset.json` 是当前仓库根目录下最重要的统一中间产物，后续难度评估和其他加工步骤都围绕它展开。

### Step 3: 计算难度分数并分层

入口：`calculate_difficulty_score/main/main.go`

#### 作用

这一步对应飞书方案中的“根据覆盖率和函数复杂度进行过滤”。当前实现里，难度分数由两部分组成：

- 依赖得分：
  - 仓库内依赖 `inhouse` 每个记 1 分
  - 外部依赖 `external` 每个记 2 分
- 函数复杂度得分：
  - 使用 `ComplexityScore()` 计算认知复杂度并直接加入总分

难度计算的读取源是根目录 `dataset.json`，输出是 `calculate_difficulty_score/dataset_handlered.json`。

#### 运行方式

```bash
cd calculate_difficulty_score
go run ./main
```

#### 输出

- `calculate_difficulty_score/dataset_handlered.json`

进一步做难度分层：

```bash
cd calculate_difficulty_score
python difficulty_score_analyze.py
```

会产生：

- `calculate_difficulty_score/dataset_handlered_difficulty_level.json`
- `calculate_difficulty_score/dataset_handlered_difficulty_level_easy.json`
- `calculate_difficulty_score/dataset_handlered_difficulty_level_hard.json`

当前脚本把阈值设为 `40`：

- `< 40` 视为 low/easy
- `>= 40` 视为 high/hard

### Step 4: 对描述字段做补充或清洗

这个仓库里有两条相邻但不完全闭环的文本处理链路。

#### 路径 A: 根目录的描述后处理链路

入口：`filter.py` 和 `generate_doc.py`

当前约定是：

1. `filter.py` 读取 `after_dataset.json`
2. 从 `function_statement` 中提取第一个 Python 代码块
3. 输出 `after1_dataset.json`
4. `generate_doc.py` 再读取 `after1_dataset.json`
5. 对每条样本的 `ground_truth` 生成伪代码，写入 `function_pseudocode`
6. 输出 `after2_dataset.json`

运行方式：

```bash
python filter.py
python generate_doc.py
```

#### 路径 B: swebench 子目录的 benchmark 整理链路

入口：`swebench/raw_dataset_process.py`

这个脚本会把原始数据集转换成更适合做生成任务的数据格式，并构造 prompt：

- `question_1`: 基于 `function_statement`
- `question_2`: 基于 `function_comment`

运行方式：

```bash
cd swebench
python raw_dataset_process.py
```

默认脚本里写死的是：

- 输入：`filter_dataset_395.json`
- 输出：`clean_benchmark_dataset.json`

如果你的实际输入不是这个文件名，需要先改脚本中的变量。

#### 关于这一步的现实情况

当前仓库并没有一个统一脚本，把根目录 `dataset.json` 自动接到 `after_dataset.json` 或 `swebench/filter_dataset_395.json`。

也就是说，这一步目前需要人工衔接：

- 你要先决定哪个 JSON 是后续实验的工作集。
- 再把文件名改成脚本期望的输入，或者修改脚本读取路径。

### Step 5: 生成模型代码

入口：`swebench/llm_generate.py`

#### 作用

该脚本会：

1. 读取 `./dataset/clean_benchmark_dataset.json`
2. 为每条样本组织 prompt
3. 调用模型生成代码
4. 将结果持续落盘到 `llm_generate_code.json`

#### 运行方式

```bash
cd swebench
python llm_generate.py
```

#### 依赖的 prompt 模板

模板位于：

- `swebench/dataset/prompts/function_comment.jinja2`
- `swebench/dataset/prompts/function_statement.jinja2`

#### 现状说明

当前 `llm_generate.py` 的 `generate_function_bodys()` 内部按 `question_1` 到 `question_3` 循环，但 `raw_dataset_process.py` 实际只构造了 `question_1` 和 `question_2`。这意味着在直接串联运行前，你最好先检查字段是否一致，避免运行期 KeyError 或空样本。

### Step 6: 回填生成代码并执行真实测试

入口：`swebench/verify.py`

#### 作用

这是最终验证环节，对应飞书方案中的“把模型生成的代码放回原仓库，在真实测试环境里判定正确性”。

核心动作包括：

1. 按 `git_repo` 聚合样本。
2. 克隆对应仓库到本地工作目录。
3. 将 `generated_code` 替换回目标文件的 `start_line:end_line`。
4. 只执行覆盖该函数的测试用例，而不是整个仓库全量测试。
5. 记录测试通过率、仓库维度统计以及按难度分层的结果。
6. 每次替换结束后恢复原始文件，避免仓库被污染。

#### 运行方式

`verify.py` 更像一个库式脚本，实际入口是 `verify_dataset_result(file_path)`。如果你要手动跑，通常需要自己指定待验证 JSON 路径。

对于 SWE-bench harness 风格的验证，仓库还提供了：

```bash
bash run_verify.sh <predictions_path>
```

#### 预期产物

验证过程中会在目标目录下生成：

- `<input_name>_results.json`
- `statistics.json`
- 分仓库处理结果目录

`swebench/dataset/qwen3_480b_a35b/function_comment/statistics.json` 和 `swebench/dataset/qwen3_480b_a35b/function_statement/statistics.json` 已经展示了这种结果形态。

## 7. 一条可执行的最小实验路径

如果你不是要重跑全量采样，而是想尽快走通一次从数据到验证的最小闭环，建议使用下面的顺序。

### 路径 1: 使用已有样本做生成与验证

1. 先检查 `swebench/dataset/go.json` 的字段结构，确认它是否满足你的 prompt 需求。
2. 运行 `swebench/raw_dataset_process.py`，把现有样本整理成 `clean_benchmark_dataset.json`。
3. 运行 `swebench/llm_generate.py`，得到模型输出文件。
4. 将模型输出整理成 `verify.py` 期望的字段格式，至少保证存在：
   - `git_repo`
   - `file_path`
   - `start_line`
   - `end_line`
   - `generated_code`
   - `testcases`
5. 调用 `swebench/verify.py` 执行回填测试。

### 路径 2: 从头重跑采样到难度分层

1. 修改 `execute.py` 中的仓库列表。
2. 运行 `bash run.sh` 批量采样。
3. 确认每个仓库都产生了 `swebench/workspace/<repo>/data/filter_dataset.json`。
4. 运行 `python collect.py` 生成根目录 `dataset.json`。
5. 进入 `calculate_difficulty_score` 目录执行 Go 难度打分。
6. 运行 `difficulty_score_analyze.py` 切分 easy/hard 子集。
7. 选定一个下游工作集，再接入生成和验证流程。

## 8. 关键产物清单

| 阶段 | 输入 | 输出 |
| --- | --- | --- |
| 仓库采样 | 仓库列表 | `swebench/workspace/<repo>/data/filter_dataset.json` |
| 全量聚合 | 每仓 `filter_dataset.json` | `dataset.json` |
| 难度评分 | `dataset.json` | `calculate_difficulty_score/dataset_handlered.json` |
| 难度分层 | `dataset_handlered.json` | easy/hard 三份 JSON |
| 描述清洗 | `after_dataset.json` | `after1_dataset.json` |
| 伪代码补充 | `after1_dataset.json` | `after2_dataset.json` |
| benchmark 整理 | 原始过滤数据 | `swebench/clean_benchmark_dataset.json` |
| LLM 生成 | `clean_benchmark_dataset.json` | `llm_generate_code.json` |
| 回仓验证 | 带 `generated_code` 的样本 | `*_results.json`、`statistics.json` |

## 9. 当前仓库的几个重要现实约束

### 9.1 不是单一入口工程

当前仓库更像一组实验脚本，而不是一个有统一 CLI 的产品化 pipeline。运行时经常需要：

- 手动切输入文件名
- 手动改路径
- 手动选择实验数据子集

### 9.2 路径有硬编码

几个明显例子：

- `calculate_difficulty_score/main/main.go` 默认从绝对路径读取根目录 `dataset.json`
- `collect.py` 假定输入目录在 `./swebench/workspace`
- `raw_dataset_process.py` 把输入文件名写死为 `filter_dataset_395.json`

如果你换机器或换目录，这些路径要先改。

### 9.3 生成与验证之间还需要格式对齐

`llm_generate.py` 的输出格式和 `verify.py` 的输入格式不是天然一一对应的。正式做实验时，建议在两者中间再加一个统一的结果转换脚本，把模型输出规整成验证脚本需要的字段。

### 9.4 推送逻辑默认存在

`execute.py` 在采样后会尝试 `git add`、`git commit`、`git push`。这对实验环境是高风险操作。如果你只想本地生成数据，应该先关闭这一段逻辑。

## 10. 推荐的下一步工程化改造

如果你准备长期维护这个仓库，建议优先做下面四件事：

1. 增加统一配置层

把仓库列表、工作目录、模型配置、输入输出文件名从脚本里抽到 YAML 或 TOML 配置文件中。

2. 增加统一 CLI

把链路统一成类似下面的命令：

```bash
python -m benchmark collect
python -m benchmark score
python -m benchmark prepare
python -m benchmark generate
python -m benchmark verify
```

3. 增加格式转换层

单独提供一个脚本，把：

- 聚合数据格式
- prompt 数据格式
- 模型输出格式
- 验证输入格式

这四类 JSON 统一映射起来。

4. 移除源码中的模型配置硬编码

改成环境变量或配置文件，否则实验迁移和审计都很困难。

## 11. 实操命令速查

### 批量采样

```bash
bash run.sh
```

### 聚合数据集

```bash
python collect.py
```

### 计算难度分数

```bash
cd calculate_difficulty_score
go run ./main
python difficulty_score_analyze.py
```

### 清洗描述并补伪代码

```bash
python filter.py
python generate_doc.py
```

### 生成 benchmark 输入

```bash
cd swebench
python raw_dataset_process.py
```

### 调模型生成代码

```bash
cd swebench
python llm_generate.py
```

### 跑验证

```bash
bash run_verify.sh <predictions_path>
```

## 12. 总结

这个仓库已经具备了函数级代码生成评测的核心骨架：

- 能从真实 Go 仓库采集函数样本。
- 能建立测试到函数的映射。
- 能保存函数依赖、函数实现和描述信息。
- 能基于依赖和复杂度做难度分层。
- 能让模型生成代码并回仓执行真实测试。

它当前最需要的不是再加一层抽象方案，而是把现有脚本之间的输入输出契约收紧，把路径和配置收敛成统一入口。只要把这一步补上，这个仓库就能从“实验脚本集合”转成一条稳定可复现的 benchmark pipeline。