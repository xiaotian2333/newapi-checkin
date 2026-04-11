package handler

import (
	"context"
	"sync"
	"time"

	"newapi-checkin/internal/store"
)

const defaultLeaderboardLimit = 10

type LeaderboardCache struct {
	store userStore
	now   func() time.Time
	limit int

	mu          sync.RWMutex
	checkinDate string
	items       []store.CheckinLeaderboardItem
}

func NewLeaderboardCache(userStore userStore, now func() time.Time, limit int) *LeaderboardCache {
	if now == nil {
		now = time.Now
	}
	if limit <= 0 {
		limit = defaultLeaderboardLimit
	}
	return &LeaderboardCache{
		store: userStore,
		now:   now,
		limit: limit,
		items: []store.CheckinLeaderboardItem{},
	}
}

func (c *LeaderboardCache) Refresh(ctx context.Context) error {
	return c.RefreshDate(ctx, c.today())
}

func (c *LeaderboardCache) RefreshDate(ctx context.Context, checkinDate string) error {
	if c == nil || c.store == nil {
		return nil
	}

	items, err := c.store.GetDailyLeaderboard(ctx, checkinDate, c.limit)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.checkinDate = checkinDate
	c.items = cloneLeaderboardItems(items)
	c.mu.Unlock()
	return nil
}

func (c *LeaderboardCache) Snapshot() (string, []store.CheckinLeaderboardItem) {
	if c == nil {
		return "", []store.CheckinLeaderboardItem{}
	}

	today := c.today()

	c.mu.RLock()
	checkinDate := c.checkinDate
	items := cloneLeaderboardItems(c.items)
	c.mu.RUnlock()

	if checkinDate == "" || checkinDate != today {
		return today, []store.CheckinLeaderboardItem{}
	}
	return checkinDate, items
}

func (c *LeaderboardCache) today() string {
	if c == nil || c.now == nil {
		return time.Now().Format("2006-01-02")
	}
	return c.now().Format("2006-01-02")
}

func cloneLeaderboardItems(items []store.CheckinLeaderboardItem) []store.CheckinLeaderboardItem {
	if len(items) == 0 {
		return []store.CheckinLeaderboardItem{}
	}

	cloned := make([]store.CheckinLeaderboardItem, len(items))
	copy(cloned, items)
	return cloned
}
