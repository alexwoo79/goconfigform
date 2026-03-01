# 动态表单系统

基于 Gin 框架的动态表单系统，支持多用户、数据隔离、配置化管理。

## 功能特性

- **动态表单配置** - 通过 YAML 文件配置表单，无需修改代码
- **多用户系统** - 支持用户注册、登录，数据隔离
- **权限管理** - 管理员可查看所有数据，普通用户只能查看自己的数据
- **数据持久化** - 用户数据保存到 YAML，表单响应保存到 CSV
- **模板嵌入** - HTML 模板编译到程序中，部署更简单
- **跨平台** - 支持 macOS 和 Windows

## 快速开始

### 运行程序

**macOS:**
```bash
./app
```

**Windows:**
```cmd
app.exe
```

程序启动后会自动打开浏览器访问登录页面。

### 默认账号

| 用户名 | 密码 | 角色 |
|--------|------|------|
| admin | admin | 管理员 |

## 使用说明

### 用户角色

| 角色 | 权限 |
|------|------|
| **管理员** | 访问配置管理页面、查看所有用户数据、切换表单配置、导出 CSV |
| **普通用户** | 填写表单、查看自己的数据 |

### 页面说明

| 页面 | 路径 | 说明 |
|------|------|------|
| 登录 | `/login` | 用户登录 |
| 注册 | `/register` | 新用户注册 |
| 表单填写 | `/form` | 填写表单 |
| 数据列表 | `/list` | 查看表单响应数据 |
| 配置管理 | `/config-manager` | 切换表单配置（管理员） |
| 配速计算 | `/pace` | 跑步配速计算器 |

### 登录后跳转

| 用户类型 | 登录后跳转 |
|----------|------------|
| 管理员 | `/config-manager` |
| 普通用户 | `/form` |

## 表单配置

### 配置文件格式

在项目目录下创建 `*_form.yaml` 文件：

```yaml
title: 表单标题
fields:
  - name: field1
    label: 字段1
    type: text
    required: true
    placeholder: 请输入内容
    
  - name: field2
    label: 字段2
    type: select
    required: true
    options:
      - value: "1"
        text: 选项1
      - value: "2"
        text: 选项2
        
  - name: field3
    label: 字段3
    type: checkbox
    options:
      - value: a
        text: 选项A
      - value: b
        text: 选项B

buttons:
  - type: submit
    text: 提交
    class: btn-primary
```

### 支持的字段类型

| 类型 | 说明 |
|------|------|
| `text` | 文本输入框 |
| `email` | 邮箱输入框 |
| `tel` | 电话输入框 |
| `number` | 数字输入框 |
| `date` | 日期选择器 |
| `time` | 时间选择器 |
| `select` | 下拉选择框 |
| `checkbox` | 复选框（多选） |
| `radio` | 单选框 |
| `textarea` | 多行文本框 |
| `range` | 滑块 |

## 数据存储

### 文件说明

| 文件 | 说明 |
|------|------|
| `form_data.db` | SQLite3 数据库（用户数据 + 表单响应） |
| `*_form.yaml` | 表单配置文件 |

### 数据库表结构

**用户表 (users):**
```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL,
    is_admin INTEGER NOT NULL DEFAULT 0
);
```

**响应表 (responses):**
```sql
CREATE TABLE responses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    config_file TEXT NOT NULL,
    username TEXT NOT NULL,
    created_at TEXT NOT NULL,
    fields TEXT NOT NULL
);
```

### 数据隔离

通过 `config_file` 和 `username` 字段实现数据隔离：

| 字段 | 说明 |
|------|------|
| `config_file` | 表单配置文件名 |
| `username` | 提交用户名 |

### 并发安全

SQLite3 数据库支持多客户端并发写入，通过数据库事务保证数据一致性，避免文件冲突。

## 部署说明

### 需要的文件

部署时只需要以下文件：

```
项目目录/
├── app 或 app.exe      # 可执行程序（已包含模板）
├── form_form.yaml      # 表单配置文件
└── form_data.db        # SQLite3 数据库（自动创建）
```

### 编译

**macOS:**
```bash
go build -o app .
```

**Windows:**
```bash
GOOS=windows GOARCH=amd64 go build -o app.exe .
```

## 技术栈

- **后端框架**: Gin
- **模板引擎**: Go html/template
- **配置格式**: YAML
- **数据存储**: SQLite3 + YAML
- **前端样式**: Bootstrap 5

## 项目结构

```
├── main.go              # 主程序
├── pace.go              # 配速计算模块
├── layout.html          # 布局模板
├── welcome.html         # 欢迎页
├── form.html            # 表单页
├── thanks.html          # 感谢页
├── list.html            # 列表页
├── config_manager.html  # 配置管理页
├── login.html           # 登录页
├── register.html        # 注册页
├── form_form.yaml       # 表单配置
├── form_data.db         # SQLite3 数据库
└── go.mod               # Go 模块配置
```

## 注意事项

1. 首次运行会自动创建 `form_data.db` 数据库和默认 admin 账号
2. 表单配置文件需要放在程序同目录下
3. 程序监听 `0.0.0.0:8080`，支持局域网访问
4. 用户密码明文存储，请勿使用重要密码
