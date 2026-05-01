package subscription

import "time"

type Status struct {
	UUID              string
	UsedTrafficBytes  int64
	TotalTrafficBytes int64
	StartDate         time.Time
	ExpireDate        *time.Time
	IsActive          bool
	SubscriptionURL   string
}

type PanelUser struct {
	UUID       string
	Name       string
	TelegramID *int64
}

func (s *Status) RemainingTrafficBytes() int64 {
	if s.TotalTrafficBytes == 0 {
		return -1
	}
	remaining := s.TotalTrafficBytes - s.UsedTrafficBytes
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (s *Status) UsedPercent() float64 {
	if s.TotalTrafficBytes == 0 {
		return 0
	}
	return float64(s.UsedTrafficBytes) / float64(s.TotalTrafficBytes) * 100
}

func (s *Status) IsExpired() bool {
	if s.ExpireDate == nil {
		return false
	}
	return time.Now().After(*s.ExpireDate)
}
