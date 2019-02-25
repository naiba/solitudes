package solitudes

import (
	"fmt"
	"testing"

	"github.com/naiba/solitudes"
)

func TestGenTOC(t *testing.T) {
	var post = &solitudes.Article{
		Content: `### 一3.1
		#### 二3.2
		## 一2.1
		# 一1.1
		##### 二5.1
		### 二3.3
		## 二2.2
		#### 三4.1
		# 一1.2
		# 一1.3`,
	}
	post.GenTOC(post)
	printToc(post.Toc, 0)
}

func printToc(toc []*solitudes.ArticleTOC, level int) {
	for _, t := range toc {
		for i := 0; i < level; i++ {
			fmt.Print(" ")
		}
		fmt.Print(t.Title, "\n")
		printToc(t.SubTitles, level+1)
	}
}
