# 发布到 GitHub 已建立仓库

当前目标仓库：`Fan-chou/magpie-native-routing`（**请确认实际 owner 是否一致**）。

若 ChatGPT GitHub 连接获得该仓库的写入权限，可直接将本仓库 Markdown 文件提交到默认分支。否则请在个人电脑中使用 Git 发布。以下示例默认仓库已经建立，且 `git` 已安装并且 GitHub CLI (`gh`) 已登录：

```bash
# 克隆现有仓库，替换为实际 owner/repo
# gh repo clone Fan-chou/magpie-native-routing
# cd magpie-native-routing

# 将从 ChatGPT 下载的 zip 解压内容（README.md、docs/、PUBLISH.md）
# 拷贝到刚克隆的仓库根目录，保留现有 .git 目录。

git status --short
git add README.md docs/ PUBLISH.md
git commit -m "docs: record Magpie Native Routing feasibility and integration plan"
git push
```

没有 `gh` 时可运行：

```bash
git clone https://github.com/Fan-chou/magpie-native-routing.git
cd magpie-native-routing
# 将 zip 内的 README.md、docs/ 和 PUBLISH.md 拷贝到此目录，然后执行上面的 add/commit/push
```

若 GitHub App 不能写仓库，请在 GitHub Settings → Applications → Installed GitHub Apps 中找到连接的 ChatGPT GitHub 应用，确认其仓库访问范围包含新仓库，并在 ChatGPT 的 GitHub 连接中完成必要授权。更改权限后再次尝试推送。

此包当前是文档项目，没有已编译/可运行客户端。上传前建议查看所有文件，尤其核对 Gateway 实际 URL 与本地 Magpie 数据路径。
