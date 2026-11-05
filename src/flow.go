package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func showFlow(engine string, out, progress io.Writer) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	width := terminalColumns(out, 80)
	if width < 8 {
		return errors.New("flow needs a terminal at least 8 columns wide")
	}
	data, err := flowSourceContext(root)
	if err != nil {
		return err
	}
	response, err := flowEngineResponse(root, fmt.Sprintf(flowPrompt, data), engine, progress)
	if err != nil {
		return fmt.Errorf("generate command flows: %q", err.Error())
	}
	flows, err := parseCommandFlows(response)
	if err != nil {
		return fmt.Errorf("command flows: %q", err.Error())
	}
	body, err := drawCommandFlows(flows, width)
	if err != nil {
		return err
	}
	current, err := flowSourceContext(root)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, current) {
		return errors.New("source changed during generation; command flows not displayed")
	}
	var context flowSourceInput
	if err := json.Unmarshal(data, &context); err != nil {
		return err
	}
	header := "Operation flows / 操作・処理フロー（LLMによる概要）\n\n"
	if context.Omitted > 0 {
		header += "※ READMEと選択したコードの範囲からの参考図です。\n\n"
	}
	_, err = fmt.Fprint(out, header+body)
	return err
}
