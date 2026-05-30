[English](./README_en.md)
# MeshBan

基于P2P的Minecraft封禁玩家名单共享。完全不依靠中心服务器，借助DHT网络和证书签名，使节点间自行建立连接并设定信任等级和封禁规则。

## 特点
- 不是插件/MOD，是独立运行的软件，我希望通过适配器来兼容所有服务器核心。
- 基于证书的节点身份，所有封禁条目可溯源。
- 在Friend-To-Friend的信任网络上分享，但也借助DHT来发现更多节点。
- 节点可以给其连接的每个MC服务器单独配置封禁规则。
- 给不同的节点设置不同的可信度，配合封禁规则达到更细腻的控制。
- 通过证书签名来推测未知节点的可信度。
- 被动式查询封禁列表，阻碍恶意封禁的扩散。
- 在玩家加入服务器后，后台查询封禁信息，若匹配封禁规则便踢出服务器，不影响玩家连接速度。

## 当前工作模式：
- 通过RCON连接Minecraft服务器执行Kick指令，通过查询banned-player.json和latest.log来获取服务器更新，尽量降低对MC性能的消耗。后续可能添加适配器来通过WebSocket主动推送更新。
- 当玩家加入服务器时，首先查找本地缓存是否有匹配的放行/踢出记录，如果没有就查找本地Banlist来计算是否达到踢出条件，如果没有达到就继续查询所有连接的Nodes，同步完本地Banlist之后再次计算，并把结果写入缓存以提升下次的查询速度。
- 添加Node：在WebUI中填写目标Node的地址，通过调用api获取Node证书，验证后保存到本地Node列表。

## WebUI
默认绑定地址是`127.0.0.1:30000`，可以在config.yaml中设置。

### 功能：
- Database：
  - Banlist相关：
    - 清除缓存
    - 手动添加Banlist记录
    - 管理本地Banlist
  - Node相关：
    - 添加Node
    - 管理Node列表
- Minecraft
  - 全局设置：
    - Minecraft连接总开关
    - 踢出消息
    - 踢出消息中的联系方式
    - 踢出标准（见下列分级）：
      - ultimate, trusted, friend, unknown, untrusted（如果本地Banlist中出现了**设定次数**个来自该信任等级的节点的玩家封禁记录，则踢出该玩家。0代表不计算这个信任等级）
    - 第三方UUID查询api：（可以用来连接非官方的UUID源，目前并未详细测试，对于目前的查询本地日志来说，没有多大用处，之后如果添加了Minecraft适配器可能需要）
      - 启用开关
      - 调用设置
      - 代理设置
  - 按Minecraft服务器设置：
    - 可覆盖的选项：踢出消息，联系方式，踢出标准。
    - 需要单独设置：RCON连接信息，`latest.log`文件地址，`banned-players.json`文件地址。（后续如果第三让UUID查询有用处也应该增加对应的覆盖选项）
  - 测试Minecraft服务器连通性。
  - 查看按Minecraft服务器实例过滤的日志。
  - 新增/删除Minecraft服务器实例。
- Identity：
  - 创建新密钥对（直接覆盖原密钥对，不可撤销）
  - 导出密钥对（不会自动解密被加密的私钥，如果加密了私钥（默认）还需要`.env`文件中的私钥密码，提前配置到目标节点才能导入成功）
  - 导入密钥对（如果导入成功将直接覆盖原密钥对，不可撤销）
- Security：
  - 更改WebUI/api绑定地址
  - 查看/设置/生成 WebUI token
  - 设置/移除 数据库中的密钥加密
- Logs：查看日志。

## 计划功能
见带有`enhancement`标签的[issues](https://github.com/PeterTerpe/MeshBan/issues)。

## 安全相关
- Node连接：MeshBan只会主动连接用户配置的Node，并默认接受所有已知或未知的Node的查询请求。后续可能添加IP黑名单以拒绝来自某些IP的请求。
- MeshBan通过Node ID来索引封禁记录的源Node身份和信任等级，所以确保本地Banlist中的Node ID无误至关重要。MeshBan在获取新的节点身份和Banlist条目时会在本地通过目标Node的公钥计算Node ID，写入本地BanList时使用本地计算的ID，并在ID不匹配时发出提醒。

## 你要参与这个项目？
最新代码请见[dev分支](https://github.com/PeterTerpe/MeshBan/tree/dev)。

[开发文档](./docs/introduction.md)

[如何贡献代码？](./docs/contribution.md)
