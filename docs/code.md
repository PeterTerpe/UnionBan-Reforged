[English](./code_en.md)
# 代码结构与用途

本文档简要说明代码结构以及每个文件的用途。
在正式加入本文档之前，相关备注可以先记录在 [notes.md](./notes.md) 中。

## main.go

主程序，用于处理其他组件。

* 初始化配置和数据库
* 创建 WebUI 服务器
* 启动已启用的 Minecraft 监控器

## api

WebUI 和 API 服务器，负责管理请求认证。

*PS：将认证逻辑移动到 auth 中可能是个不错的想法*

## auth

[api](#api) 的认证辅助模块。

* 处理 WebUI 连接认证（基于 token/session cookie）
* 防止针对 WebUI token 的基础暴力破解攻击

## config

初始化/加载配置文件。

* 如果 `config.yaml` 不存在，则根据 `example_config.yaml` 创建它。

## database

处理存储在数据库中的数据。

### banlist.go

处理 banlist 表。

* 在写入数据库前验证条目

  * 必需字段：玩家 UUID、封禁原因、来源节点 ID

### cache.go

处理 Minecraft 加入检查所使用的玩家决策缓存条目。

* 按 Minecraft 实例和玩家 UUID 缓存放行/踢出决策
* 当本地 banlist 版本发生变化时，使已缓存的决策失效

### identity.go

使用 [identity/service.go](#servicego) 处理节点身份。

* 本地身份
* 其他身份（尚未实现）

## debug/peer

用于在开发过程中测试 peer 连接的调试工具，目前尚未完全实现。

## minecraft

通过日志跟踪和 RCON 监控 Minecraft 服务器。

* 跟踪已配置的服务器日志，并在出现玩家加入事件时检查玩家
* 通过 RCON 轮询 `banlist players`，并将可解析的服务器封禁导入本地 banlist
* 从日志中的 UUID 行、RCON 实体数据或可选的 UUID 解析器中解析玩家 UUID
* 在评估本地 banlist 条目前，先检查已缓存的放行/踢出决策
* 踢出满足已配置策略的玩家

## identity

直接处理密钥对。

### private_key.go

处理私钥加密、解密和版本管理。它是 [service.go](#servicego) 的辅助模块。

### service.go

管理本地身份、签名和私钥。

* 从数据库初始化本地身份
* 初始化 `.env` 文件中的私钥密码
* 密钥对导入/导出

  * 验证导入的 JSON
* 获取/重新获取其他节点的信息

## secrets

生成新的密码/token，并将它们保存到 `.env`。

## web

用户交互的部分，但所有流量都会先经过 [api](#api) 中的 `adminAccessMiddleware()` 进行认证。

它允许用户：

* 登录
* 管理本地身份：

  * 导入/导出/重新生成密钥对
* 管理 WebUI 安全选项：

  * 绑定地址
  * 复制/设置/重新生成 token
  * 切换远程客户端的 token 验证
* 管理密钥安全选项：

  * 移除/设置私钥密码
* 管理本地 Banlist 条目：

  * 添加/编辑
* 调试选项
* 查看日志
* 管理 Minecraft 服务器日志/RCON 连接和状态
* 管理 peer 连接：（待实现）

  * 管理 peer 列表
  * 管理信任等级
  * 拒绝来自黑名单 IP 的请求

### static & templates

用于构建前端 WebUI 的 CSS 和 HTML 文件。

### web.go

构建 WebUI 前端。
