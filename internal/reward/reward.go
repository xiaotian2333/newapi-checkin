package reward

import (
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
)

const MinQuotaIncrementStep = int64(5000)

func NormalizeQuotaIncrementRange(min, max int64) (int64, int64, error) {
	if min <= 0 {
		return 0, 0, errors.New("QUOTA_INCREMENT_MIN 必须大于 0")
	}
	if max <= 0 {
		return 0, 0, errors.New("QUOTA_INCREMENT_MAX 必须大于 0")
	}
	if min > max {
		return 0, 0, errors.New("QUOTA_INCREMENT_MIN 不能大于 QUOTA_INCREMENT_MAX")
	}

	effectiveMin := roundUpToStep(min, MinQuotaIncrementStep)
	effectiveMax := roundDownToStep(max, MinQuotaIncrementStep)
	if effectiveMin > effectiveMax {
		return 0, 0, fmt.Errorf("QUOTA_INCREMENT_MIN 和 QUOTA_INCREMENT_MAX 之间至少要包含一个 %d 的倍数", MinQuotaIncrementStep)
	}
	return effectiveMin, effectiveMax, nil
}

func RandomQuotaIncrement(min, max int64) (int64, error) {
	return randomQuotaIncrement(cryptorand.Reader, min, max)
}

func randomQuotaIncrement(reader io.Reader, min, max int64) (int64, error) {
	if reader == nil {
		reader = cryptorand.Reader
	}

	effectiveMin, effectiveMax, err := NormalizeQuotaIncrementRange(min, max)
	if err != nil {
		return 0, err
	}

	stepCount := (effectiveMax - effectiveMin) / MinQuotaIncrementStep
	if stepCount == 0 {
		return effectiveMin, nil
	}

	offset, err := cryptorand.Int(reader, big.NewInt(stepCount+1))
	if err != nil {
		return 0, fmt.Errorf("生成签到额度随机值失败: %w", err)
	}
	return effectiveMin + offset.Int64()*MinQuotaIncrementStep, nil
}

func roundUpToStep(value, step int64) int64 {
	remainder := value % step
	if remainder == 0 {
		return value
	}
	return value + step - remainder
}

func roundDownToStep(value, step int64) int64 {
	return value - value%step
}
