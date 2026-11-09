package service

import (
	"fmt"
	"strings"
)

const (
	OpenAIInventoryMaxRows  = 10000
	OpenAIInventoryMaxBytes = 8 << 20
)

// ParseOpenAIInventoryLines validates the whole import before any credentials
// reach storage. Error messages contain only line numbers, never source text.
func ParseOpenAIInventoryLines(raw string) ([]AccountTokenGuardReloginAccount, error) {
	if len(raw) > OpenAIInventoryMaxBytes {
		return nil, fmt.Errorf("库存文件超过 %d MiB", OpenAIInventoryMaxBytes>>20)
	}
	lines := strings.Split(raw, "\n")
	rows := make([]AccountTokenGuardReloginAccount, 0, min(len(lines), OpenAIInventoryMaxRows))
	seen := make(map[string]struct{}, min(len(lines), OpenAIInventoryMaxRows))
	for index, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(rows) == OpenAIInventoryMaxRows {
			return nil, fmt.Errorf("单次最多导入 %d 条账号", OpenAIInventoryMaxRows)
		}
		parts := strings.Split(line, "----")
		if len(parts) != 3 {
			return nil, fmt.Errorf("第 %d 行格式不正确", index+1)
		}
		row := AccountTokenGuardReloginAccount{
			Email:     strings.ToLower(strings.TrimSpace(parts[0])),
			Password:  strings.TrimSpace(parts[1]),
			MFASecret: strings.TrimSpace(parts[2]),
		}
		if err := ValidateOpenAITwoFALogin(row); err != nil {
			return nil, fmt.Errorf("第 %d 行账号或凭据无效", index+1)
		}
		if _, exists := seen[row.Email]; exists {
			return nil, fmt.Errorf("第 %d 行邮箱重复", index+1)
		}
		seen[row.Email] = struct{}{}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("库存内容为空")
	}
	return rows, nil
}
