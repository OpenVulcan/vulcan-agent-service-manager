# Vulcan Agent Service Manager

`vasm` 是独立于 Vulcan Agent Service 的 Go 安装器与终端管理器。两个仓库分别构建、打标和发布；引导脚本只下载管理器，管理器再下载主程序完整发布包。

## 一键打开安装器

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/OpenVulcan/vulcan-agent-service-manager/main/scripts/install.ps1 | iex
```

Linux / macOS：

```sh
curl -fsSL https://raw.githubusercontent.com/OpenVulcan/vulcan-agent-service-manager/main/scripts/install.sh | sh
```

默认从 GitHub 下载。使用国内代理预设时，Windows 先设置 `$env:VASM_SOURCE='mirror'`，Unix 在脚本后附加 `-s -- --source mirror`；也可设置 `VASM_MIRROR_BASE` 或传入 `--mirror-base https://你的代理域名`。当前预设为 `https://gh-proxy.com`，属于第三方服务，必须主动选择。镜像只传输大文件；版本和 SHA-256 以 GitHub 官方 Release API 或官方 `.sha256` 文件为准。如果官方元数据不可达，安装会明确失败，不会暗中降低校验强度。

脚本将管理器安装至 Windows `%LOCALAPPDATA%\OpenVulcan\vasm\bin\vasm.exe` 或 Unix `~/.local/bin/vasm`，然后打开 TUI。加入用户 PATH 由向导选项或 `vasm path add` 负责，新终端才会读取更新后的 PATH。脚本可通过 `VASM_VERSION=v0.1.4` 或 Unix `--version v0.1.4` 固定管理器版本。

## 管理命令

直接运行 `vasm` 打开安装向导和管理菜单。等价命令行入口包括：

```text
vasm --version
vasm check-updates
vasm install --yes [--source github|mirror] [--mirror-base HTTPS_URL] [--app-version v0.1.0]
vasm install --yes --service --scope system --startup auto --start --init-skills --skills vulcan-codekit,vulcan-file --add-path
vasm adopt --runtime-root ABSOLUTE_PATH [--service-installed --scope system]
vasm status [--json]
vasm run
vasm start | stop | restart
vasm service status | install | uninstall | startup auto | startup manual
vasm skills list --layer ROOT|USER
vasm skills install GITHUB_SOURCE --layer ROOT|USER
vasm skills update [SKILL_ID] --layer ROOT|USER
vasm skills uninstall SKILL_ID --layer USER
vasm config show
vasm config set vmm_enable true|false
vasm config set vmm HTTP_URL
vasm config budget default BYTES
vasm config budget CLIENT_NAME BYTES
vasm config skill NAME true|false
vasm config skill @auto-install true|false
vasm source show | set github | set mirror [HTTPS_PROXY_BASE]
vasm path show | add | remove
vasm update [--app-version TAG]
vasm update-self [--manager-version TAG]
vasm doctor [--json]
vasm uninstall [--purge --yes]
```

HTTP/SSE 客户端使用 `Vulcan-Client-Match-Name` 请求头携带客户端名称；`config budget CLIENT_NAME BYTES` 把这个精确值映射到现有的工具结果字节上限。`config skill @auto-install false` 会保留用户主动禁用的系统技能自动安装状态。Windows 原生服务仅支持系统作用域，需要管理员权限；前台模式不需要安装服务。

首次安装的 `--skills` 可以使用 `default`（保留发布包默认选择）、`none`（全部禁用）或逗号分隔的技能名称。管理器在已校验的发布包清单中核对每个名称，未知名称会终止安装。安装后可通过 TUI 的配置页或 `vasm config skill NAME true|false` 修改单项，ROOT 技能清单可用 `vasm skills list --layer ROOT` 查看。

`vasm update` 获取主程序最新正式 Release，并在版本未变化时跳过下载。`vasm update-self` 独立检查管理器最新正式 Release、校验对应资产后更新管理器；Windows 使用退出后的辅助进程替换运行中的程序。公开 GitHub API 被限流时，可选地提供 `GITHUB_TOKEN` 仅用于官方元数据请求；镜像仍只传输归档。

升级安装在原目录旁暂存和验证新包，保留配置、日志以及 LuaSkills 技能和状态，初始化失败会恢复旧包。`vasm uninstall` 默认保留这些用户数据；只有明确传入 `--purge --yes` 才删除整个受管运行目录。卸载主程序不会移除管理器或其 PATH 条目，下载源和 PATH 选择保存在独立的管理器偏好文件中。

## 发布与开发

管理器版本从 `0.1.0` 开始。`.github/workflows/release.yml` 仅允许手动执行并选择**已有标签**，在 Windows x64、Linux x64/ARM64、macOS Intel/ARM 上分别进行原生测试和打包。发布资产固定为 `vasm-<platform>.zip` 或 `.tar.gz`，每份均有 `.sha256` 文件，并附带 `SHA256SUMS`。推送标签本身不会发布。主程序 Release 独立维护。

```sh
go test ./...
go vet ./...
python -m unittest discover -s scripts/tests -p 'test_release.py' -v
```

完整功能边界和后续跨仓库契约见[设计方案](docs/design/vasm-manager-design.md)，实施进度见 `docs/plan/`。
