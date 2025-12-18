# SoutuBot-go

## 要求

- Go 1.25+
- 运行中的 FlareSolverr 服务端 (fsClient 传 nil 也能跑, 能不能用看 IP 质量)

## 安装

```bash
go get github.com/Miuzarte/SoutuBot-go
```

## 示例

```go
package main

import (
    "context"
    "fmt"
    fs "github.com/Miuzarte/FlareSolverr-go"
    stb "github.com/Miuzarte/SoutuBot-go"
)

func main() {
    fsClient := fs.NewClient("http://127.0.0.1:8191/v1")
    client := stb.NewClient(fsClient)

    var imgData []byte // raw png/jpg/webp
    resp, err := client.Search(context.Background(), imgData)
    if err != nil {
        panic(err)
    }

    fmt.Printf("上传的原图片: %s\n", resp.ImageUrl)
    fmt.Printf(
        "耗时 %.2fs\n%s\n找到了 %d 条相似的结果\n",
        resp.ExecutionTime,
        resp.SearchOption,
        len(resp.Data),
    )
    if len(resp.Data) == 0 {
        return
    }
    if resp.Data[0].Similarity < stb.MATCH_SIMILARITY_THRESHOLD {
        fmt.Println("最大匹配度低于45，结果可能不正确")
        fmt.Println("请自行判断，或更换严格模式/其他搜图引擎来搜索")
    }

    var lastI int
    for i, item := range resp.Data {
        if item.Similarity < stb.LOW_SIMILARITY_THRESHOLD {
            // 匹配度必定按从大到小排序
            if i > 3 {
                // 保留前三条低匹配度结果, 剩余的不展示
                break
            }
        }

        var hosts [2]string = item.Source.Hosts()
        // [0]: "https://nhentai.net"  / [1]: "https://nhentai.xxx"
        // [0]: "https://e-hentai.org" / [1]: "https://exhentai.org"

        fmt.Printf(
            "[%d] %s\n匹配度: %.2f%%\n语言: %s\n来源: %s\n",
            i, item.Title,
            item.Similarity,
            item.Language.Emoji(),
            item.Source,
        )
        fmt.Printf("详情页1: %s\n", hosts[0]+item.SubjectPath)
        fmt.Printf("详情页2: %s\n", hosts[1]+item.SubjectPath)
        fmt.Printf("图片页1: %s\n", hosts[0]+item.PagePath)
        fmt.Printf("图片页2: %s\n", hosts[1]+item.PagePath)

        lastI = i
    }

    if lastI+1 < len(resp.Data) {
        fmt.Printf("未显示剩余低匹配度结果(%d条)\n", len(resp.Data)-lastI-1),
    }
}
```