# 更新日志

## 0.1.107

首个可直接下载安装的版本。

- 提供 macOS 通用版（Apple 芯片与 Intel 同一份安装包），无需自己编译。
- 内置自动更新：启动 10 秒后检查一次，此后每天检查一次，发现新版本会显示更新说明并可一键安装、重启生效。
- 更新包使用独立密钥签名，应用只接受签名匹配的更新。

安装：从最新 release 下载 dmg，把 Taskboard 拖进「应用程序」，然后在终端执行一次
`xattr -dr com.apple.quarantine /Applications/Taskboard.app` 解除未签名应用的隔离标记。
也可以直接用一键安装脚本：

```sh
curl -fsSL https://github.com/ttmouse/taskboard-native/releases/latest/download/install-macos.sh | sh
```

## 0.1.106

- 派发诊断日志拆分到运行日志，T3 探测读取发布端口。
