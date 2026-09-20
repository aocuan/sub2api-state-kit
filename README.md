# Sub2API STATE Kit

这是一个面向官方 Sub2API `v0.2.7` 的独立 `OpenAI OAuth` transport 插件，在 `v0.3.3` 基础上维护并开源。插件 ID 保持为 `io.github.wangyunjeff.sub2api-state-kit`，可从既有版本停用后覆盖升级。

当前签名发布包包含 Linux amd64、Linux arm64 和 macOS arm64 运行时。

## 功能

- 账号选择框显示账号 ID、名称和邮箱。
- 账号列表显示并保存账号 ID、名称、邮箱、到期时间和额度摘要。
- 模型字段提供常用模型建议，同时允许输入自定义模型名。
- 运行状态显示 `账号：ID + 名称` 和 `模型：模型名`。
- 保留按账号启用的 Pro / Team STATE 管理、动态代理采集、固定业务代理复验、续期和异常守护。
- 第一层代理可直接选择 Sub2API IP 管理中的代理，第二层填写动态代理地址。

账号选择框和资料列表通过宿主配置页的只读 Bridge 从账号管理同步 ID、名称、邮箱、订阅到期时间和容量摘要，不会把 OAuth Token、API Key、密码或完整凭据传入插件页面。官方原版宿主接口只返回账号 ID；此时插件会退化为只显示 ID，不会生成“未填写名称 / 未填写邮箱”占位文字。

## 安装与升级

1. 下载 [`sub2api-state-kit_plugin_v0.3.4.s2plugin`](https://github.com/zhang2580384/sub2api-state-kit/releases/tag/v0.3.4)。
2. 在 Sub2API 的 `插件管理` 中上传并启用插件。
3. 第一层代理从 IP 管理中选择；第二层填写 HTTP(S) 或 SOCKS5(H) 动态代理地址。
4. 添加需要处理的账号，补全展示资料，选择 Pro / Team 和模型，再开启账号与总开关。

升级同 ID 插件时，宿主会要求先停用当前版本，再上传新版本并重新启用。插件自身升级不需要修改或重启 Sub2API 宿主容器。

完整说明见 [插件安装与使用](docs/plugin.md)，测试和验收范围见 [验证记录](docs/plugin-validation.md)。

## 开发

```bash
cd plugin
go test -race ./...
go vet ./...
node --test ui-tests/*.test.cjs
go build -trimpath -o ../build/state-kit ./cmd/state-kit
```

签名构建脚本位于 `scripts/package_plugin.py`。签名私钥必须保存在仓库外，不能提交到仓库或 Release。

## 安全

仓库和发布包不包含真实账号、邮箱、OAuth Token、STATE、API Key、代理密码、数据库或签名私钥。生产配置请只保存在 Sub2API 实例中。

## 上游与许可

- Sub2API: <https://github.com/Wei-Shaw/sub2api>
- 设计参考: <https://github.com/gylive/ccodex-sleep-state>

本项目是非官方扩展，与 Sub2API 作者无隶属或背书关系。许可证为 LGPL-3.0，相关归属见 `LICENSE`、`NOTICE` 和 `COPYING.GPL3`。
