package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const commandFlowExample = `[{"command":"generate","steps":["依頼を入力","AIが提案を生成","保存","レビュー開始"]},{"command":"revise","steps":["対象を選択","履歴を読む","AIが再構成","保存"],"note":"レビューは別途開始"}]`

func TestFlowJapaneseCharacterWidths(t *testing.T) {
	for text, want := range map[string]int{"エラー": 6, "エラーを表示": 12, "メニュー・戻る": 14, "ASCII": 5} {
		if got := flowTextWidth(text); got != want {
			t.Errorf("width of %q: %d, want %d", text, got, want)
		}
	}
}

func TestParseCommandFlows(t *testing.T) {
	flows, err := parseCommandFlows([]byte(commandFlowExample))
	if err != nil || len(flows) != 2 || len(flows[0].Steps) != 4 {
		t.Fatalf("flows: %v, %v", flows, err)
	}
	for _, input := range []string{
		"", "[]", "null", "{}", "\xff", commandFlowExample + "[]",
		`[{"command":"","steps":["a"]}]`,
		`[{"command":"x","steps":[]}]`,
		`[{"command":"x","steps":[""]}]`,
		`[{"command":"x","steps":["a","b","c","d","e","f","g"]}]`,
		`[{"command":"x","steps":["a"],"sources":[1]}]`,
		`[{"command":"x","steps":["a"]},{"command":"x","steps":["b"]}]`,
		`[{"command":"x\u001b[2J","steps":["a"]}]`,
		`[{"command":"x","steps":["a\n"]}]`,
		`[{"command":"x","steps":["a\u202e"]}]`,
		`[{"command":"x","steps":["a"],"note":"\u001b[31m"}]`,
		`[{"command":"x","steps":["\ufffd"]}]`,
		strings.Repeat(" ", maxFlowResponse+1),
	} {
		if flows, err := parseCommandFlows([]byte(input)); err == nil {
			t.Fatalf("accepted invalid input %q: %v", input, flows)
		}
	}
	for _, count := range []int{32, 33} {
		flows := make([]commandFlow, count)
		for i := range flows {
			flows[i] = commandFlow{Command: strings.Repeat("x", i+1), Steps: []string{"表示"}}
		}
		data, err := json.Marshal(flows)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseCommandFlows(data); (err == nil) != (count == 32) {
			t.Fatalf("flow count %d: %v", count, err)
		}
	}
	for _, flow := range []commandFlow{
		{Command: strings.Repeat("x", 129), Steps: []string{"a"}},
		{Command: "x", Steps: []string{strings.Repeat("x", 257)}},
		{Command: "x", Steps: []string{"a"}, Note: strings.Repeat("x", 257)},
	} {
		data, _ := json.Marshal([]commandFlow{flow})
		if _, err := parseCommandFlows(data); err == nil {
			t.Fatal("accepted oversized label")
		}
	}
}

func TestParseCommandFlowsBranches(t *testing.T) {
	valid := commandFlow{Command: "レビュー開始", Steps: []string{"入力", "保存できたか"}, Branches: []flowBranch{
		{Label: "成功", Steps: []string{"保存", "開始"}},
		{Label: "失敗", Steps: []string{"エラー表示"}},
	}}
	for _, count := range []int{2, 3} {
		flow := valid
		flow.Branches = append([]flowBranch(nil), valid.Branches...)
		if count == 3 {
			flow.Branches = append(flow.Branches, flowBranch{Label: strings.Repeat("a", 64), Steps: []string{"a", "b", strings.Repeat("x", 256)}})
		}
		data, _ := json.Marshal([]commandFlow{flow})
		got, err := parseCommandFlows(data)
		if err != nil || !reflect.DeepEqual(got, []commandFlow{flow}) {
			t.Fatalf("branch round trip: %v, %v", got, err)
		}
	}
	cases := map[string][]flowBranch{
		"one":  {{Label: "yes", Steps: []string{"a"}}},
		"four": {{Label: "a", Steps: []string{"a"}}, {Label: "b", Steps: []string{"a"}}, {Label: "c", Steps: []string{"a"}}, {Label: "d", Steps: []string{"a"}}},
	}
	for name, change := range map[string]func(*flowBranch){
		"empty label":       func(b *flowBranch) { b.Label = "" },
		"blank label":       func(b *flowBranch) { b.Label = "　 " },
		"duplicate label":   func(b *flowBranch) { b.Label = "成功" },
		"trimmed duplicate": func(b *flowBranch) { b.Label = " 成功 " },
		"long label":        func(b *flowBranch) { b.Label = strings.Repeat("a", 65) },
		"CJK byte limit":    func(b *flowBranch) { b.Label = strings.Repeat("成", 22) },
		"no steps":          func(b *flowBranch) { b.Steps = nil },
		"four steps":        func(b *flowBranch) { b.Steps = []string{"a", "b", "c", "d"} },
		"empty step":        func(b *flowBranch) { b.Steps = []string{""} },
		"blank step":        func(b *flowBranch) { b.Steps = []string{"　"} },
		"long step":         func(b *flowBranch) { b.Steps = []string{strings.Repeat("a", 257)} },
	} {
		branches := append([]flowBranch(nil), valid.Branches...)
		change(&branches[1])
		cases[name] = branches
	}
	for _, bad := range []string{"\x1b[31m", "a\n", "a\t", "a\x7f", "a\u009b", "a\u202e", "a\u2066", "\ufffd", "\xff"} {
		cases["unsafe label "+bad] = []flowBranch{valid.Branches[0], {Label: bad, Steps: []string{"a"}}}
		cases["unsafe step "+bad] = []flowBranch{valid.Branches[0], {Label: "failure", Steps: []string{bad}}}
	}
	for name, branches := range cases {
		t.Run(name, func(t *testing.T) {
			flow := valid
			flow.Branches = branches
			data, _ := json.Marshal([]commandFlow{flow})
			if _, err := parseCommandFlows(data); err == nil {
				t.Fatal("accepted invalid branch JSON")
			}
			if out, err := drawCommandFlows([]commandFlow{flow}, 80); err == nil || out != "" {
				t.Fatal("rendered invalid branch Go value")
			}
		})
	}
	for _, input := range []string{
		`[{"command":"x","steps":["check"],"branches":[{"label":"yes","steps":["ok"],"nodes":[]},{"label":"no","steps":["stop"]}]}]`,
		`[{"command":"x","steps":["check"],"branches":[{"label":"yes","steps":["ok"],"branches":[]},{"label":"no","steps":["stop"]}]}]`,
	} {
		if _, err := parseCommandFlows([]byte(input)); err == nil {
			t.Fatal("accepted unknown branch field")
		}
	}
}

func TestDrawCommandFlowsBoxesAndArrows(t *testing.T) {
	flows := []commandFlow{{Command: "generate", Steps: []string{"入力", "保存"}}}
	got, err := drawCommandFlows(flows, 80)
	want := "generate\n┌──────┐\n│ 入力 │\n└──────┘\n    │\n    ↓\n┌──────┐\n│ 保存 │\n└──────┘\n\n"
	if err != nil || got != want {
		t.Fatalf("diagram:\n%s\nwant:\n%s\nerror: %v", got, want, err)
	}
	t.Log("\n" + got)
}

func TestDrawCommandFlowsBranchSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name     string
		width    int
		branches []flowBranch
		want     string
	}{
		{"two unequal", 18, []flowBranch{{Label: "yes", Steps: []string{"save", "done"}}, {Label: "no", Steps: []string{"stop"}}}, `run
     ┌──────┐
     │ test │
     └──────┘
         │
    ┌────┴────┐
  yes        no
    ↓         ↓
┌──────┐  ┌──────┐
│ save │  │ stop │
└──────┘  └──────┘
    │
    ↓
┌──────┐
│ done │
└──────┘

`},
		{"three CJK", 28, []flowBranch{{Label: "成功", Steps: []string{"保存"}}, {Label: "失敗", Steps: []string{"中止"}}, {Label: "取消", Steps: []string{"戻る"}}}, `run
          ┌──────┐
          │ test │
          └──────┘
              │
    ┌─────────┼─────────┐
  成功      失敗      取消
    ↓         ↓         ↓
┌──────┐  ┌──────┐  ┌──────┐
│ 保存 │  │ 中止 │  │ 戻る │
└──────┘  └──────┘  └──────┘

`},
		{"narrow tree", 10, []flowBranch{{Label: "成功", Steps: []string{"保存", "開始"}}, {Label: "失敗", Steps: []string{"中止"}}}, `run
┌──────┐
│ test │
└──────┘
    │
┌───┘
├─ 成功
│     ↓
│ ┌──────┐
│ │ 保存 │
│ └──────┘
│     │
│     ↓
│ ┌──────┐
│ │ 開始 │
│ └──────┘
└─ 失敗
      ↓
  ┌──────┐
  │ 中止 │
  └──────┘

`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := drawCommandFlows([]commandFlow{{Command: "run", Steps: []string{"test"}, Branches: tc.branches}}, tc.width)
			if err != nil || got != tc.want {
				t.Fatalf("got:\n%swant:\n%s error: %v", got, tc.want, err)
			}
			if tc.name != "narrow tree" {
				wide, err := drawCommandFlows([]commandFlow{{Command: "run", Steps: []string{"test"}, Branches: tc.branches}}, 80)
				if err != nil || wide != tc.want {
					t.Fatalf("width 80 must stay compact:\n%s error: %v", wide, err)
				}
				if tc.name == "three CJK" {
					t.Log("width 80 Japanese branches:\n" + wide)
				}
			}
		})
	}
}

func TestFlowBranchLayout(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		width, mainWidth, count          int
		label, step                      string
		column, mainIndent, branchIndent int
	}{
		{"two compact", 80, 8, 2, "成功", "保存", 8, 5, 0},
		{"three compact", 80, 8, 3, "成功", "保存", 8, 10, 0},
		{"main wider", 80, 40, 2, "成功", "保存", 8, 0, 11},
		{"odd column", 80, 8, 2, "yes", "store", 9, 6, 0},
		{"label sets width", 80, 8, 2, "long condition", "ok", 14, 11, 0},
		{"CJK sets width", 80, 8, 2, "成功", "正常に保存", 14, 11, 0},
		{"capped column", 30, 8, 3, "条件", strings.Repeat("x", 30), 8, 10, 0},
		{"narrow unchanged", 17, 8, 2, "成功", "保存", 7, 0, 0},
		{"straight unchanged", 80, 8, 0, "", "", 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			branches := make([]flowBranch, tc.count)
			for i := range branches {
				branches[i] = flowBranch{Label: tc.label, Steps: []string{tc.step}}
			}
			column, mainIndent, branchIndent := flowBranchLayout(branches, tc.width, tc.mainWidth)
			if column != tc.column || mainIndent != tc.mainIndent || branchIndent != tc.branchIndent {
				t.Fatalf("layout = %d, %d, %d; want %d, %d, %d", column, mainIndent, branchIndent, tc.column, tc.mainIndent, tc.branchIndent)
			}
		})
	}
}

func TestDrawCommandFlowsCenteredSteps(t *testing.T) {
	for _, tc := range []struct {
		name, lastStep                 string
		mainIndent, branchIndent, root int
	}{
		{"main narrower", "test", 5, 0, 9},
		{"main wider", strings.Repeat("x", 36), 0, 11, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flow := commandFlow{Command: "run", Steps: []string{"open", tc.lastStep}, Branches: []flowBranch{
				{Label: "成功", Steps: []string{"保存"}},
				{Label: "失敗", Steps: []string{"中止"}},
			}}
			out, err := drawCommandFlows([]commandFlow{flow}, 80)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(out, "\n")
			for _, row := range []int{1, 6} {
				if !strings.HasPrefix(lines[row], strings.Repeat(" ", tc.mainIndent)+"┌") {
					t.Fatalf("main step not centered: %q", lines[row])
				}
			}
			for _, row := range []int{4, 9} {
				if lines[row] != strings.Repeat(" ", tc.root)+"│" {
					t.Fatalf("main connector not centered: %q", lines[row])
				}
			}
			if lines[11] != strings.Repeat(" ", tc.branchIndent+2)+"成功      失敗" ||
				lines[13] != strings.Repeat(" ", tc.branchIndent)+"┌──────┐  ┌──────┐" {
				t.Fatalf("branches not centered:\n%s", out)
			}
		})
	}
}

func TestDrawCommandFlowsBranchesWidths(t *testing.T) {
	for _, label := range []string{strings.Repeat("a", 62), strings.Repeat("条件", 10)} {
		for _, count := range []int{2, 3} {
			flow := commandFlow{Command: "TUI 操作開始", Steps: []string{"入力を検証する", strings.Repeat("判定", 20)}, Note: "補足"}
			for i := 0; i < count; i++ {
				flow.Branches = append(flow.Branches, flowBranch{Label: label + strings.Repeat("x", i), Steps: []string{strings.Repeat("処理", 40)}})
			}
			for width := 8; width <= 81; width++ {
				out, err := drawCommandFlows([]commandFlow{flow}, width)
				if err != nil {
					t.Fatalf("width %d: %v", width, err)
				}
				for _, line := range strings.Split(out, "\n") {
					if flowTextWidth(line) > min(width, 80) {
						t.Fatalf("width %d exceeded: %q", width, line)
					}
				}
				if width < count*8+(count-1)*2 && (!strings.Contains(out, "├─ ") || !strings.Contains(out, "└─ ")) {
					t.Fatalf("missing sibling tree at width %d: %s", width, out)
				}
			}
		}
	}
}

func TestCommandFlowsSizeLimits(t *testing.T) {
	input := []byte(commandFlowExample)
	input = append(input, []byte(strings.Repeat(" ", maxFlowResponse-len(input)))...)
	if _, err := parseCommandFlows(input); err != nil {
		t.Fatalf("64 KiB input rejected: %v", err)
	}
	if _, err := parseCommandFlows(append(input, ' ')); err == nil {
		t.Fatal("accepted input over 64 KiB")
	}
	flows := make([]commandFlow, 32)
	for i := range flows {
		flows[i] = commandFlow{Command: strings.Repeat("x", 128-i), Note: strings.Repeat("x", 256)}
		for j := 0; j < 6; j++ {
			flows[i].Steps = append(flows[i].Steps, strings.Repeat("x", 256))
		}
		for j := 0; j < 3; j++ {
			flows[i].Branches = append(flows[i].Branches, flowBranch{Label: strings.Repeat("x", 64-j), Steps: []string{strings.Repeat("x", 256), strings.Repeat("x", 256), strings.Repeat("x", 256)}})
		}
	}
	for _, width := range []int{8, 9, 17, 18, 27, 28, 80, 1000} {
		out, err := drawCommandFlows(flows, width)
		if err != nil || len(out) > 1<<20 || len(out) == 0 {
			t.Fatalf("output bound: %d bytes, %v", len(out), err)
		}
		t.Logf("width %d: %d bytes (limit %d)", width, len(out), 1<<20)
	}
}

func TestDrawCommandFlowsWrapsAndKeepsSimpleOutput(t *testing.T) {
	flows, err := parseCommandFlows([]byte(commandFlowExample))
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{8, 12, 40, 80, 1000} {
		out, err := drawCommandFlows(flows, width)
		if err != nil || strings.Count(out, "┌") != 8 || strings.Count(out, "↓") != 6 || strings.Contains(out, "sources") {
			t.Fatalf("width %d: %q, %v", width, out, err)
		}
		for _, line := range strings.Split(out, "\n") {
			columns := 0
			for _, r := range line {
				columns += displayWidth(r)
			}
			if columns > min(width, 80) {
				t.Fatalf("width %d exceeded: %q", width, line)
			}
		}
	}
	if out, err := drawCommandFlows(flows, 7); err == nil || out != "" {
		t.Fatal("accepted narrow output")
	}
	if out, err := drawCommandFlows([]commandFlow{{Command: "x", Steps: []string{"\x1b"}}}, 80); err == nil || out != "" {
		t.Fatal("rendered unsafe Go value")
	}
}
