package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func loadDotEnv(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("读取 %s 失败: %w", path, err)
	}

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	for index, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if index == 0 {
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s 第 %d 行格式非法", path, index+1)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("%s 第 %d 行缺少变量名", path, index+1)
		}

		parsedValue, err := parseDotEnvValue(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("%s 第 %d 行解析失败: %w", path, index+1, err)
		}

		// 显式注入的系统环境变量优先级更高，不被 .env 覆盖。
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, parsedValue); err != nil {
			return fmt.Errorf("写入环境变量 %s 失败: %w", key, err)
		}
	}

	return nil
}

func parseDotEnvValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}

	if strings.HasPrefix(value, "\"") {
		if len(value) < 2 || !strings.HasSuffix(value, "\"") {
			return "", errors.New("双引号未闭合")
		}
		replacer := strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\t`, "\t", `\"`, `"`, `\\`, `\`)
		return replacer.Replace(value[1 : len(value)-1]), nil
	}

	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return "", errors.New("单引号未闭合")
		}
		return value[1 : len(value)-1], nil
	}

	if commentIndex := strings.Index(value, " #"); commentIndex >= 0 {
		value = value[:commentIndex]
	}
	return strings.TrimSpace(value), nil
}
