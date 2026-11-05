package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxFlowResponse = 64 << 10

type commandFlow struct {
	Command  string       `json:"command"` // CLI name or TUI operation title.
	Steps    []string     `json:"steps"`   // With branches, the last step is the condition.
	Note     string       `json:"note,omitempty"`
	Branches []flowBranch `json:"branches,omitempty"`
}

type flowBranch struct {
	Label string   `json:"label"`
	Steps []string `json:"steps"`
}

func parseCommandFlows(response []byte) ([]commandFlow, error) {
	if len(response) > maxFlowResponse || !utf8.Valid(response) {
		return nil, errors.New("invalid command flow size or encoding")
	}
	var flows []commandFlow
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&flows); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("expected one command flow JSON array")
	}
	if err := validateCommandFlows(flows); err != nil {
		return nil, err
	}
	return flows, nil
}

func validateCommandFlows(flows []commandFlow) error {
	if len(flows) == 0 || len(flows) > 32 {
		return errors.New("expected 1..32 command flows")
	}
	safe := func(text string, limit int) bool {
		return len(text) <= limit && utf8.ValidString(text) && strings.IndexFunc(text, func(r rune) bool {
			return unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) || r == utf8.RuneError
		}) < 0
	}
	seen := map[string]bool{}
	for _, flow := range flows {
		if strings.TrimSpace(flow.Command) == "" || !safe(flow.Command, 128) || seen[flow.Command] || len(flow.Steps) == 0 || len(flow.Steps) > 6 || !safe(flow.Note, 256) {
			return errors.New("invalid command flow name, steps, or note")
		}
		seen[flow.Command] = true
		if len(flow.Branches) != 0 && (len(flow.Branches) < 2 || len(flow.Branches) > 3) {
			return errors.New("expected 2..3 flow branches")
		}
		labels := map[string]bool{}
		for _, branch := range flow.Branches {
			label := strings.TrimSpace(branch.Label)
			if label == "" || !safe(branch.Label, 64) || labels[label] || len(branch.Steps) < 1 || len(branch.Steps) > 3 {
				return errors.New("invalid flow branch label or steps")
			}
			labels[label] = true
			for _, step := range branch.Steps {
				if strings.TrimSpace(step) == "" || !safe(step, 256) {
					return errors.New("invalid flow branch step")
				}
			}
		}
		for _, step := range flow.Steps {
			if strings.TrimSpace(step) == "" || !safe(step, 256) {
				return errors.New("invalid command flow step")
			}
		}
	}
	return nil
}

func drawCommandFlows(flows []commandFlow, width int) (string, error) {
	if width < 8 {
		return "", errors.New("flow needs at least 8 columns")
	}
	if err := validateCommandFlows(flows); err != nil {
		return "", err
	}
	width = min(width, 80)
	var out strings.Builder
	for _, flow := range flows {
		out.WriteString(strings.Join(wrapFlowText(flow.Command, width), "\n") + "\n")
		boxWidth := flowBoxWidth(flow.Steps, width)
		_, mainIndent, _ := flowBranchLayout(flow.Branches, width, boxWidth)
		out.WriteString(drawFlowSteps(flow.Steps, boxWidth, mainIndent))
		if len(flow.Branches) > 0 {
			out.WriteString(drawFlowBranches(flow.Branches, width, boxWidth))
		}
		if flow.Note != "" {
			out.WriteString(strings.Join(wrapFlowText("※ "+flow.Note, width), "\n") + "\n")
		}
		out.WriteByte('\n')
	}
	if out.Len() > 1<<20 {
		return "", errors.New("command flow diagram exceeds 1 MiB")
	}
	return out.String(), nil
}

func flowTextWidth(text string) int {
	columns := 0
	for _, r := range text {
		columns += displayWidth(r)
	}
	return columns
}

func flowBoxWidth(steps []string, width int) int {
	boxWidth := min(8, width)
	for _, step := range steps {
		boxWidth = max(boxWidth, min(width, flowTextWidth(step)+4))
	}
	return boxWidth
}

func drawFlowSteps(steps []string, boxWidth, indent int) string {
	var out strings.Builder
	prefix := strings.Repeat(" ", indent)
	inner := boxWidth - 4
	border := strings.Repeat("─", boxWidth-2)
	for i, step := range steps {
		if i > 0 {
			padding := prefix + strings.Repeat(" ", boxWidth/2)
			out.WriteString(padding + "│\n" + padding + "↓\n")
		}
		out.WriteString(prefix + "┌" + border + "┐\n")
		for _, line := range wrapFlowText(step, inner) {
			out.WriteString(prefix + "│ " + line + strings.Repeat(" ", inner-flowTextWidth(line)) + " │\n")
		}
		out.WriteString(prefix + "└" + border + "┘\n")
	}
	return out.String()
}

func flowBranchLayout(branches []flowBranch, width, mainBoxWidth int) (columnWidth, mainIndent, branchIndent int) {
	if len(branches) == 0 {
		return 0, 0, 0
	}
	needed := 8
	for _, branch := range branches {
		needed = max(needed, flowTextWidth(branch.Label))
		for _, step := range branch.Steps {
			needed = max(needed, flowTextWidth(step)+4)
		}
	}
	columnWidth = min((width-2*(len(branches)-1))/len(branches), needed)
	if columnWidth < 8 {
		return columnWidth, 0, 0
	}
	span := len(branches)*columnWidth + 2*(len(branches)-1)
	diagramWidth := max(mainBoxWidth, span)
	return columnWidth, (diagramWidth - mainBoxWidth) / 2, (diagramWidth - span) / 2
}

func drawFlowBranches(branches []flowBranch, width, mainBoxWidth int) string {
	var out strings.Builder
	columnWidth, mainIndent, branchIndent := flowBranchLayout(branches, width, mainBoxWidth)
	root := mainIndent + mainBoxWidth/2
	out.WriteString(strings.Repeat(" ", root) + "│\n")
	if columnWidth < 8 {
		out.WriteString("┌" + strings.Repeat("─", root-1) + "┘\n")
		for i, branch := range branches {
			fork, trunk := "├─ ", "│ "
			if i == len(branches)-1 {
				fork, trunk = "└─ ", "  "
			}
			for j, line := range wrapFlowText(branch.Label, width-3) {
				prefix := trunk + " "
				if j == 0 {
					prefix = fork
				}
				out.WriteString(prefix + line + "\n")
			}
			boxWidth := flowBoxWidth(branch.Steps, width-2)
			out.WriteString(trunk + strings.Repeat(" ", boxWidth/2) + "↓\n")
			for _, line := range strings.Split(strings.TrimSuffix(drawFlowSteps(branch.Steps, boxWidth, 0), "\n"), "\n") {
				out.WriteString(trunk + line + "\n")
			}
		}
		return out.String()
	}

	first := columnWidth / 2
	last := branchIndent + (len(branches)-1)*(columnWidth+2) + first
	left, right := min(root, branchIndent+first), max(root, last)
	fork := []rune(strings.Repeat(" ", left) + strings.Repeat("─", right-left+1))
	fork[left], fork[right] = '┌', '┐'
	fork[root] = '┴'
	if root == left {
		fork[root] = '└'
	} else if root == right {
		fork[root] = '┘'
	}
	for i := range branches {
		center := branchIndent + i*(columnWidth+2) + first
		fork[center] = '┬'
		if center == left {
			fork[center] = '┌'
		} else if center == right {
			fork[center] = '┐'
		}
		if center == root {
			fork[center] = '┼'
			if center == left {
				fork[center] = '├'
			} else if center == right {
				fork[center] = '┤'
			}
		}
	}
	out.WriteString(string(fork) + "\n")
	columns := make([][]string, len(branches))
	labelHeight := 0
	for i, branch := range branches {
		columns[i] = wrapFlowText(branch.Label, columnWidth)
		for j, line := range columns[i] {
			columns[i][j] = strings.Repeat(" ", (columnWidth-flowTextWidth(line))/2) + line
		}
		labelHeight = max(labelHeight, len(columns[i]))
	}
	height := 0
	for i, branch := range branches {
		for len(columns[i]) < labelHeight {
			columns[i] = append(columns[i], "")
		}
		columns[i] = append(columns[i], strings.Repeat(" ", first)+"↓")
		columns[i] = append(columns[i], strings.Split(strings.TrimSuffix(drawFlowSteps(branch.Steps, columnWidth, 0), "\n"), "\n")...)
		height = max(height, len(columns[i]))
	}
	for row := 0; row < height; row++ {
		var line strings.Builder
		line.WriteString(strings.Repeat(" ", branchIndent))
		for i, column := range columns {
			text := ""
			if row < len(column) {
				text = column[row]
			}
			line.WriteString(text)
			if i < len(columns)-1 {
				line.WriteString(strings.Repeat(" ", columnWidth-flowTextWidth(text)+2))
			}
		}
		out.WriteString(strings.TrimRight(line.String(), " ") + "\n")
	}
	return out.String()
}

func wrapFlowText(text string, width int) []string {
	var lines []string
	var line strings.Builder
	columns := 0
	for _, r := range text {
		w := displayWidth(r)
		if columns+w > width {
			lines = append(lines, line.String())
			line.Reset()
			columns = 0
		}
		line.WriteRune(r)
		columns += w
	}
	return append(lines, line.String())
}
