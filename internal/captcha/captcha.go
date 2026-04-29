package captcha

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrInvalidToken       = errors.New("验证码校验失败，请重试")
	ErrServiceUnavailable = errors.New("验证码服务暂时不可用，请稍后重试")
	ErrUnknownProvider    = errors.New("未知的验证码服务商")
)

type Provider interface {
	Verify(ctx context.Context, token, remoteIP, expectedAction string) error
}

type ProviderFactory func(secretKey string) (Provider, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]ProviderFactory)
)

func Register(name string, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

type Service struct {
	provider Provider
}

func NewService(captchaType, secretKey string) (*Service, error) {
	registryMu.RLock()
	factory, ok := registry[captchaType]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, captchaType)
	}
	provider, err := factory(secretKey)
	if err != nil {
		return nil, err
	}
	return &Service{provider: provider}, nil
}

func (s *Service) Verify(ctx context.Context, token, remoteIP, expectedAction string) error {
	if s == nil || s.provider == nil {
		return ErrServiceUnavailable
	}
	return s.provider.Verify(ctx, token, remoteIP, expectedAction)
}