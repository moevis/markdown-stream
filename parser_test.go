package markdown

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var testMarkdown = `
上海有许多好玩的地方，以下是一些推荐：

1. **东方明珠广播电视塔**：作为上海的标志性建筑之一，东方明珠塔是国家首批AAAAA级旅游景区。塔内设有太空舱、旋转餐厅和上海城市历史发展陈列馆等景观和设施<web_7df48932>。

2. **上海外滩**：外滩是上海的地标性景点，拥有丰富的历史和文化。在这里，你可以欣赏到上海的天际线和黄浦江的美景<web_81d380c0>。

3. **1192弄老上海风情街**：这条街道完美地重现了老上海的风情，让你仿佛穿越回了过去的时光<web_7df48932>。

4. **新场古镇**：这是一个保存完好的古镇，拥有许多古建筑和传统风貌<web_debe60ca>。

5. **上海海昌海洋公园**：这是一个适合家庭出游的地方，有各种海洋生物和游乐设施<web_debe60ca>。

6. **周浦花海**：这里是赏花的好去处，你可以欣赏到各种美丽的花海<web_debe60ca>。

7. **上海野生动物园**：这是中国首座国家级野生动物园，你可以近距离观察各种野生动物<web_debe60ca>。

8. **南京路步行街**：这是上海最繁华的商业街之一，你可以在这里购物、品尝美食，并感受上海的城市氛围<web_99bdceee>。

以上是一些上海好玩的地方，希望对你有所帮助！如果你有更多的兴趣和时间，上海还有许多其他值得一游的景点。

### 示例标题（有三个#）

这是公式 $公式$ 这是引用 <doc_12345>。 ` + "`行内代码块，支持转义反引号 \\` `" + `

公式 
$$
\begin{aligned}
x &= y + z \\
\end{aligned}
$$

无序列表：
- 项目1
- 项目2
+ 项目1
+ 项目2
* 项目1
* 项目2

 > 引用文本1
 > 引用文本2

  > 引用文本2

这是一个链接到[GitHub](https://github.com)的链接。

#### 图片

这是一张图片：
![Markdown Logo](https://markdown-here.com/img/icon256.png)

` + "```python" + `
print("Hello, world!")
` + "```" + `

#### 强调

*斜体文本*
**粗体文本**

#### 表格

| 列1 | 列2 | 列3 |
| --- | --- | --- |
| 行1列1 | 行1列2 | 行1列3 |
| 行2列1 | 行2列2 | 行2列3 |

全文完
`

func lightGreyString(s string) string {
	return "\033[90m" + s + "\033[0m"
}

func TestMarkdown(t *testing.T) {
	parser := NewParser()
	go parser.Start(MarkdownDoc)
	parser.Put(testMarkdown)

	parser.Close()

	var lastType MarkdownSegment

	buff := bytes.NewBuffer(nil)
	allBuff := bytes.NewBuffer(nil)

	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		allBuff.WriteRune(seg.Rune)
		if lastType.Rune == 0 {
			lastType = seg
		}
		if lastType.Type != seg.Type {
			content := buff.String()
			if content != "" {
				fmt.Printf("%s:%s\n", lightGreyString(string(lastType.Type)), content)
			}
			buff.Reset()
			lastType = seg
		}
		buff.WriteRune(seg.Rune)
	}

	content := buff.String()
	if content != "" {
		fmt.Printf("%s:[%s]", lightGreyString(string(lastType.Type)), content)
	}
	fmt.Println(allBuff.String())
}

func TestEquationElem(t *testing.T) {
	parser := NewParser()
	go parser.Start(
		MarkdownElem{
			Type:         MarkdownDocElemType,
			TrimWhites:   []rune{' ', '\t', '\n'},
			SubElems:     []MarkdownElem{ListElem},
			TextElemType: MarkdownDocElemType.Text(),
		},
	)
	input := `// 代码内容：读“此处是代码”
// - 数学公式：
//   - 行内公式：按微软 TTS 能力读“$...$”中的内容，如$E=mc^2$
//   - 独立公式：读“此处是公式”，如$$\int_{-\infty}^{+\infty} e^{-x^2} dx = \sqrt{\pi}$$
//`
	buff := bytes.NewBuffer(nil)
	parser.Put(input)
	parser.Close()
	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		buff.WriteRune(seg.Rune)
		fmt.Printf("%s:%s\n", seg.Type, string(seg.Rune))
	}
	fmt.Println(buff.String())
}

func TestTableElem(t *testing.T) {
	parser := NewParser()
	go parser.Start(TableElem)
	parser.Put("| 列1 | 列2 | 列3 |\n| --- | --- | --- |\n| 行1列1 | 行1列2 | 行1列3 |\n| 行2列1 | 行2列2 | 行2列3 |")
	parser.Close()
	buf := bytes.NewBuffer(nil)
	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		buf.WriteRune(seg.Rune)
		fmt.Printf("%s:%s\n", seg.Type, string(seg.Rune))
	}
	fmt.Println(buf.String())
}

func TestCodeElem(t *testing.T) {
	parser := NewParser()
	go parser.Start(CodeElem)
	parser.Put("```python\n" +
		"print(\"Hello, world!\")\n" +
		"```")
	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		fmt.Printf("%s:%s\n", seg.Type, string(seg.Rune))
	}
}

func TestIndexReference(t *testing.T) {
	parser := NewParser()
	go parser.Start(IndexElem)
	parser.Put("<web_1234>")
	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		fmt.Printf("%s:%s\n", seg.Type, string(seg.Rune))
	}
}

func TestListElem(t *testing.T) {
	parser := NewParser()
	go parser.Start(
		MarkdownElem{
			TrimWhites: []rune{' ', '\t', '\n'},
			SubElems:   []MarkdownElem{ListElem},
			Inline:     false,
		},
	)
	parser.Put("- 项目1\n- 项目2\n+ 项目1\n+ 项目2\n* 项目1\n* 项目2")
	parser.Close()
	buf := bytes.NewBuffer(nil)
	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		buf.WriteRune(seg.Rune)
		fmt.Printf("%s:%s\n", seg.Type, string(seg.Rune))
	}

	fmt.Println(buf.String())
}

func TestStrongElem(t *testing.T) {
	parser := NewParser()
	go parser.Start(ParagraphElem)
	parser.Put("**H<web_1>**：1")
	parser.Close()
	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		fmt.Printf("%s:%s\n", seg.Type, string(seg.Rune))
	}
}

func TestLargePayload(t *testing.T) {
	parser := NewParser()
	go parser.Start(MarkdownDoc)
	parser.Put(strings.Repeat("*", 100000) + "hello" + strings.Repeat("*", 999))
	parser.Close()
	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		fmt.Printf("%s:%s\n", seg.Type, string(seg.Rune))
	}
}

func Test_MyParser(t *testing.T) {
	text := `上海有许多好玩的地方，以下是一些推荐：
##东方明珠广播电视塔
1. **东方明珠广播电视塔**：作为上海的标志性建筑之一，东方明珠塔是国家首批AAAAA级旅游景区。塔内设有太空舱、旋转餐厅和上海城市历史发展陈列馆等景观和设施<web_7df48932>。

2. **上海外滩**：外滩是上海的地标性景点，拥有丰富的历史和文化。在这里，你可以欣赏到上海的天际线和黄浦江的美景<web_81d380c0>。

3. **1192弄老上海风情街**：这条街道完美地重现了老上海的风情，让你仿佛穿越回了过去的时光<web_7df48932>。

4. **新场古镇**：这是一个保存完好的古镇，拥有许多古建筑和传统风貌<web_debe60ca>。

5. **上海海昌海洋公园**：这是一个适合家庭出游的地方，有各种海洋生物和游乐设施<web_debe60ca>。

6. **周浦花海**：这里是赏花的好去处，你可以欣赏到各种美丽的花海<web_debe60ca>。

7. **上海野生动物园**：这是中国首座国家级野生动物园，你可以近距离观察各种野生动物<web_debe60ca>。

8. **南京路步行街**：这是上海最繁华的商业街之一，你可以在这里购物、品尝美食，并感受上海的城市氛围<web_99bdceee>。

以上是一些上海好玩的地方，希望对你有所帮助！如果你有更多的兴趣和时间，上海还有许多其他值得一游的景点。`
	parser := NewParser()
	go parser.Start(MarkdownDoc)
	parser.Put(text)
	parser.Close()
	buf := bytes.NewBuffer(nil)
	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		fmt.Printf("%s:%s\n", seg.Type, string(seg.Rune))
		buf.WriteRune(seg.Rune)
	}
	fmt.Println(buf.String())
}

func Test_Parser(t *testing.T) {
	text1 := `嗨嗨嗨`
	text2 := `上海有许多好玩的地方：*`
	text3 := `*哈哈哈哈`
	parser := NewParser()
	go parser.Start(MarkdownDoc)
	parser.Put(text1)
	go func() {
		time.Sleep(time.Second * 2)
		parser.Put(text2)
	}()
	go func() {
		time.Sleep(time.Second * 6)
		parser.Put(text3)
		parser.Close()
	}()
	buf := bytes.NewBuffer(nil)
	for {
		seg, ok := parser.ReadNonBlocking()
		if !ok {
			break
		}
		if seg != nil {
			buf.WriteRune(seg.Rune)
			fmt.Printf("%s:%s:%d:%d\n", seg.Type, string(seg.Rune), parser.PushState().index, utf8.RuneCountInString(buf.String()))
		} else {
			fmt.Printf("%s\n", "nil")
		}
	}
	fmt.Println(buf.String())
}
