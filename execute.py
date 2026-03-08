#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import os
import subprocess
import shutil
import sys
from pathlib import Path


def run_command(command, cwd=None):
    """实时显示命令输出的执行函数"""
    print(f"正在执行: {command}")
    print("-" * 50)

    try:
        # 使用 Popen 来实时捕获输出
        process = subprocess.Popen(
            command,
            shell=True,
            cwd=cwd,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,  # 将stderr重定向到stdout
            universal_newlines=True,
            bufsize=1
        )

        # 实时读取并显示输出
        output_lines = []
        while True:
            output = process.stdout.readline() if process.stdout else None
            if output == '' and process.poll() is not None:
                break
            if output:
                print(output.strip())
                output_lines.append(output.strip())

        # 等待进程结束
        return_code = process.wait()

        print("-" * 50)
        if return_code == 0:
            print(f"✓ 命令执行成功")
            return True
        else:
            print(f"✗ 命令执行失败，返回码: {return_code}")
            return False

    except Exception as e:
        print(f"✗ 执行命令时发生异常: {str(e)}")
        return False


def clone_repository(repo_url, target_dir):
    """克隆 GitHub 仓库"""
    print(f"\n正在克隆仓库: {repo_url}")

    # 如果目录已存在，先删除
    if os.path.exists(target_dir):
        shutil.rmtree(target_dir)

    command = f"git clone {repo_url} {target_dir}"
    return run_command(command)


def create_generate_folder(repo_dir, source_files):
    """在仓库中创建 generate 文件夹并复制文件"""
    generate_dir = os.path.join(repo_dir, "generate")

    print(f"\n在 {repo_dir} 中创建 generate 文件夹")

    # 创建 generate 目录
    os.makedirs(generate_dir, exist_ok=True)

    # 复制源文件到 generate 目录
    for source_file in source_files:
        if os.path.exists(source_file):
            filename = os.path.basename(source_file)
            target_file = os.path.join(generate_dir, filename)
            shutil.copy2(source_file, target_file)
            print(f"✓ 已复制文件: {filename} -> {target_file}")
        else:
            print(f"✗ 源文件不存在: {source_file}")
            return False

    return True


def execute_go_generate(repo_dir):
    """执行 go run ./generate/*.go 命令"""
    print(f"\n在 {repo_dir} 中执行 Go 命令")

    # 检查是否存在 .go 文件
    generate_dir = os.path.join(repo_dir, "generate")
    go_files = list(Path(generate_dir).glob("*.go"))

    if not go_files:
        print("✗ generate 文件夹中没有找到 .go 文件")
        return False

    command = "go run ./generate/*.go"
    return run_command(command, cwd=repo_dir)


def push_changes(repo_dir, commit_message="Add generate folder and execute go generate"):
    """提交并推送更改到 GitHub"""
    print(f"\n推送 {repo_dir} 的更改到 GitHub")

    # 添加所有更改
    if not run_command("git add .", cwd=repo_dir):
        return False

    # 检查是否有更改需要提交
    result = subprocess.run(
        "git status --porcelain",
        shell=True,
        cwd=repo_dir,
        capture_output=True,
        text=True
    )

    if not result.stdout.strip():
        print("没有更改需要提交")
        return True

    # 提交更改
    commit_cmd = f'git commit -m "{commit_message}"'
    if not run_command(commit_cmd, cwd=repo_dir):
        return False

    # 推送到远程仓库
    return run_command("git push origin main || git push origin master", cwd=repo_dir)


def process_repositories(repo_urls, source_files, workspace_dir="./workspace"):
    """处理所有仓库的主函数"""

    # 创建工作目录
    os.makedirs(workspace_dir, exist_ok=True)

    # 检查源文件是否存在
    for source_file in source_files:
        if not os.path.exists(source_file):
            print(f"✗ 源文件不存在: {source_file}")
            return False

    success_count = 0
    total_count = len(repo_urls)

    for i, repo_url in enumerate(repo_urls, 1):
        print(f"\n{'='*60}")
        print(f"处理仓库 {i}/{total_count}: {repo_url}")
        print(f"{'='*60}")

        # 从 URL 中提取仓库名
        repo_name = repo_url.rstrip('/').split('/')[-1]
        if repo_name.endswith('.git'):
            repo_name = repo_name[:-4]

        repo_dir = os.path.join(workspace_dir, repo_name)

        try:
            # 步骤1: 克隆仓库
            if os.path.exists(repo_dir):
                print(f"✗ 跳过仓库 {repo_name}: 目录已存在")
            elif not clone_repository(repo_url, repo_dir):
                print(f"✗ 跳过仓库 {repo_name}: 克隆失败")
                continue

            # 步骤2: 创建 generate 文件夹并复制文件
            if not create_generate_folder(repo_dir, source_files):
                print(f"✗ 跳过仓库 {repo_name}: 创建 generate 文件夹失败")
                continue

            if not run_command("go mod tidy", cwd=repo_dir):
                print(f"✗ 跳过仓库 {repo_dir}: 执行 go mod tidy 失败")
                continue
            print(f"✓ 仓库 {repo_dir} 执行 go mod tidy 成功")

            # 步骤3: 执行 go run 命令
            if not execute_go_generate(repo_dir):
                print(f"✗ 跳过仓库 {repo_name}: 执行 Go 命令失败")
                continue

            # 步骤4: 推送更改
            if not push_changes(repo_dir):
                print(f"✗ 跳过仓库 {repo_name}: 推送更改失败")
                continue

            success_count += 1
            print(f"✓ 仓库 {repo_name} 处理完成")

        except Exception as e:
            print(f"✗ 处理仓库 {repo_name} 时发生错误: {str(e)}")
            continue

    print(f"\n{'='*60}")
    print(f"处理完成: {success_count}/{total_count} 个仓库成功处理")
    print(f"{'='*60}")

    return success_count == total_count


def main():
    """主函数"""

    # 配置区域 - 请根据您的需求修改以下内容

    # GitHub 仓库列表
    repositories = [
        # "git@github.com:ai4se-boys/echo.git", ✅ 7
        # "git@github.com:ai4se-boys/fiber.git", ✅ 291
        # "git@github.com:ai4se-boys/viper.git", ✅ 52
        # "git@github.com:ai4se-boys/beego.git", ✅ 43
        # "git@github.com:ai4se-boys/gin.git", ✅ 12
        # "git@github.com:ai4se-boys/gogs.git", # ✅ 160
        # "https://github.com/ai4se-boys/grpc-go",  # ✅ 23
        # "git@github.com:ai4se-boys/go-swagger.git", # ✅ 102
        # "git@github.com:ai4se-boys/hugo.git"
        # "git@github.com:ai4se-boys/go-micro.git" # ✅ 103
        # 770 条

        # 共 794 条
        # "git@github.com:ai4se-boys/frp.git" ✅ 9
        # 共 803 条
        # "git@github.com:ai4se-boys/ollama.git" ✅ 19
        # 共 822 条
        # "git@github.com:ai4se-boys/ent.git" ✅ 120
        # 共 942 条
        # "git@github.com:ai4se-boys/go-micro.git",
        # "git@github.com:ai4se-boys/oauth2.git",
        # "git@github.com:ai4se-boys/frp.git",
        # "git@github.com:ai4se-boys/ollama.git",
        # "git@github.com:ai4se-boys/ent.git", 122
        # "git@github.com:ai4se-boys/livekit.git", 95
        # "git@github.com:ai4se-boys/dapr.git", 149
        # "git@github.com:kubernetes/kubernetes.git",
        # "git@github.com:moby/moby.git",
        # # "git@github.com:base/node.git",
        # "git@github.com:caddyserver/caddy.git",
        # # "git@github.com:minio/minio.git",
        # # "git@github.com:etcd-io/etcd.git",
        # # "git@github.com:traefik/traefik.git",
        # "git@github.com:hashicorp/terraform.git"
        # "git@github.com:helm/helm.git"
        # "git@github.com:dgraph-io/dgraph.git",
        # "git@github.com:redis/go-redis.git",
        # "git@github.com:charmbracelet/gum.git",
        # "git@github.com:gitleaks/gitleaks.git",
        # "git@github.com:cilium/cilium.git",
        # "git@github.com:valyala/fasthttp.git",
        # "git@github.com:microsoft/typescript-go.git",
        # "git@github.com:uber-go/zap.git",
        # "git@github.com:openfaas/faas.git",
        # "git@github.com:grafana/loki.git",
        # "git@github.com:binwiederhier/ntfy.git",
        # "git@github.com:jesseduffield/lazydocker.git",
        # "git@github.com:wagoodman/dive.git",
        # "git@github.com:syncthing/syncthing.git"
        # "https://github.com/v2ray/v2ray-core.git",
        # "https://github.com/cockroachdb/cockroach.git",
        # "git@github.com:iawia002/lux.git",
        # "https://github.com/air-verse/air.git",
        # "https://github.com/rancher/rancher.git",
        # "https://github.com/go-delve/delve.git",
        # "https://github.com/asdf-vm/asdf.git",
        # "https://github.com/v2ray/v2ray-core.git",
        # "https://github.com/mudler/LocalAI.git",
        # "git@github.com:charmbracelet/bubbletea.git",
        # "https://github.com/slimtoolkit/slim.git",
        # "https://github.com/tmrts/go-patterns.git"
        "https://github.com/trustwallet/assets.git",
        "https://github.com/ethereum/go-ethereum.git",
        "https://github.com/hyperledger/fabric.git",
        "https://github.com/kubernetes/ingress-nginx.git",
        "https://github.com/IBAX-io/go-ibax.git",
        "https://github.com/kubernetes/minikube.git",
        "https://github.com/hashicorp/vault.git",
        "https://github.com/go-kratos/kratos.git",
        "https://github.com/XTLS/Xray-core.git",
        "https://github.com/kubernetes/kops.git",
        "https://github.com/cosmos/cosmos-sdk.git",
        "https://github.com/gorilla/websocket.git",
        "https://github.com/adonovan/gopl.io.git",
        "https://github.com/prometheus/node_exporter.git",
        "https://github.com/k3s-io/k3s.git",
        "https://github.com/OpenNHP/opennhp.git",
        "https://github.com/kgretzky/evilginx2.git",
        "https://github.com/kubesphere/kubesphere.git",
        "https://github.com/jaegertracing/jaeger.git",
        "https://github.com/go-admin-team/go-admin.git"
    ]

    # 需要复制到 generate 文件夹的源文件路径
    source_files = [
        "/home/baixuran/code/workspace/gosrc/main/main.go",  # 请替换为实际的文件路径
        "/home/baixuran/code/workspace/gosrc/main/lsp.go",  # 请替换为实际的文件路径
    ]

    # 工作目录（克隆的仓库将保存在这里）
    workspace = "./workspace"

    # 检查必要的工具
    required_tools = ["git", "go"]
    for tool in required_tools:
        if shutil.which(tool) is None:
            print(f"✗ 未找到必要工具: {tool}")
            print("请确保已安装 Git 和 Go，并且在 PATH 中可访问")
            sys.exit(1)

    print("开始处理 GitHub 仓库...")
    print(f"总共需要处理 {len(repositories)} 个仓库")
    print(f"源文件: {source_files}")
    print(f"工作目录: {workspace}")

    # repositories.reverse()
    # 开始处理
    success = process_repositories(repositories, source_files, workspace)

    if success:
        print("\n🎉 所有仓库处理完成!")
    else:
        print("\n⚠️  部分仓库处理失败，请检查上述错误信息")


if __name__ == "__main__":
    main()
    # execute_go_generate("./workspace/gin")
