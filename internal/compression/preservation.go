package compression

import (
	"fmt"
	"strings"
)

// preservedBlock holds a placeholder and its original content.
type preservedBlock struct {
	marker  string
	content string
}

// ExtractPreservedBlocks replaces code blocks, URLs, and JSON objects
// with placeholders so compressors never touch them.
func ExtractPreservedBlocks(text string) (string, []preservedBlock) {
	lines := strings.Split(text, "\n")
	var blocks []preservedBlock
	var outLines []string
	var inCodeBlock bool
	var codeBuf strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Fenced code block boundary
		if strings.HasPrefix(trimmed, "```") {
			if inCodeBlock {
				codeBuf.WriteString(line)
				blocks = append(blocks, preservedBlock{
					marker:  fmt.Sprintf("__OMNI_PRESERVE_%d__", len(blocks)),
					content: codeBuf.String(),
				})
				outLines = append(outLines, blocks[len(blocks)-1].marker)
				codeBuf.Reset()
				inCodeBlock = false
				continue
			}
			inCodeBlock = true
			codeBuf.Reset()
			codeBuf.WriteString(line)
			codeBuf.WriteString("\n")
			continue
		}

		if inCodeBlock {
			codeBuf.WriteString(line)
			codeBuf.WriteString("\n")
			continue
		}

		// Inline URL preservation
		if strings.Contains(line, "http://") || strings.Contains(line, "https://") {
			blocks = append(blocks, preservedBlock{
				marker:  fmt.Sprintf("__OMNI_PRESERVE_%d__", len(blocks)),
				content: line,
			})
			outLines = append(outLines, blocks[len(blocks)-1].marker)
			continue
		}

		outLines = append(outLines, line)
	}

	// If still in code block at EOF, preserve what we have
	if inCodeBlock && codeBuf.Len() > 0 {
		blocks = append(blocks, preservedBlock{
			marker:  fmt.Sprintf("__OMNI_PRESERVE_%d__", len(blocks)),
			content: codeBuf.String(),
		})
		outLines = append(outLines, blocks[len(blocks)-1].marker)
	}

	return strings.Join(outLines, "\n"), blocks
}

// RestorePreservedBlocks swaps placeholders back for their original content.
func RestorePreservedBlocks(text string, blocks []preservedBlock) string {
	for _, b := range blocks {
		text = strings.Replace(text, b.marker, b.content, 1)
	}
	return text
}
