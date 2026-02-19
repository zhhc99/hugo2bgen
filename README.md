# hugo2bgen

只是一个简单工具, 将 hugo posts 转换到 bgen.

- 删除多余 frontmatters
- 移动封面图到合适位置
- 保留 bundle 结构
- 不修改原始文件
- 飞快

> 未必对所有 hugo 文件结构都有效, 但希望能产生帮助 ♥️.

## 🛠 如何使用

安装:

```bash
# 推荐使用 go install
go install github.com/zhhc99/hugo2bgen@latest
```

使用:

```bash
cd path-to-my-blog-repo
hugo2bgen
# 会忽略无法转换的文件. 检查输出和 output 文件夹.
```
