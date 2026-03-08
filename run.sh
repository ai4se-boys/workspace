#!/bin/bash
set -x
# 安装适配当前Go版本的gopls
install_gopls() {
    # 检查Go是否存在
    if ! command -v go &> /dev/null; then
        echo "错误: 未找到 Go 编译器"
        return 1
    fi

    # 获取Go版本
    local go_version=$(go version | awk '{print $3}' | sed 's/go//' | cut -d. -f1,2)
    echo "当前 Go 版本: $go_version"

    # 选择gopls版本
    local gopls_version
    case $go_version in
        1.2[0-9])  gopls_version="v0.15.3" ;;  # Go 1.20+
        1.1[8-9])  gopls_version="v0.14.2" ;;  # Go 1.18-1.19
        1.17)      gopls_version="v0.11.0" ;;  # Go 1.17
        1.1[5-6])  gopls_version="v0.9.5" ;;   # Go 1.15-1.16
        1.1[2-4])  gopls_version="v0.7.5" ;;   # Go 1.12-1.14
        *)
            echo "错误: Go 版本 $go_version 不支持，请升级到 Go 1.12+"
            return 1
            ;;
    esac

    echo "选择 gopls 版本: $gopls_version"

    # 检查是否已安装相同版本
    if command -v gopls &> /dev/null; then
        local current_version=$(gopls version 2>/dev/null | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+' || echo "unknown")
        if [ "$current_version" = "$gopls_version" ]; then
            echo "已安装正确版本的 gopls ($gopls_version)"
            return 0
        fi
    fi

    # 安装gopls
    echo "正在安装 gopls $gopls_version..."
    if [[ "$go_version" > "1.16" ]] || [[ "$go_version" == "1.17" ]]; then
        go install golang.org/x/tools/gopls@$gopls_version
    else
        local temp_dir=$(mktemp -d)
        (cd "$temp_dir" && GO111MODULE=on go get golang.org/x/tools/gopls@$gopls_version)
        rm -rf "$temp_dir"
    fi

    # 验证安装
    if command -v gopls &> /dev/null; then
        echo "✓ gopls 安装成功: $(gopls version | head -n1)"
        return 0
    else
        echo "✗ gopls 安装失败"
        return 1
    fi
}



if install_gopls; then
    echo "gopls安装完成，继续后续操作..."
else
    echo "gopls安装失败，退出"
    exit 1
fi

python execute.py