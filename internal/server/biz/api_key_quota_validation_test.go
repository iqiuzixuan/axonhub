package biz

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/objects"
)

func TestValidateProfileQuota_PastDurationMinuteAccepted(t *testing.T) {
	err := validateProfileQuota([]objects.APIKeyProfile{
		{
			Name: "p",
			Quota: &objects.APIKeyQuota{
				Requests: lo.ToPtr(int64(1)),
				Period: objects.APIKeyQuotaPeriod{
					Type: objects.APIKeyQuotaPeriodTypePastDuration,
					PastDuration: &objects.APIKeyQuotaPastDuration{
						Value: 1,
						Unit:  objects.APIKeyQuotaPastDurationUnitMinute,
					},
				},
			},
		},
	})
	require.NoError(t, err)
}

func TestValidateProfileQuota_CalendarUnits(t *testing.T) {
	for _, unit := range []objects.APIKeyQuotaCalendarDurationUnit{
		objects.APIKeyQuotaCalendarDurationUnitDay,
		objects.APIKeyQuotaCalendarDurationUnitWeek,
		objects.APIKeyQuotaCalendarDurationUnitMonth,
		"year",
	} {
		t.Run(string(unit), func(t *testing.T) {
			err := validateProfileQuota([]objects.APIKeyProfile{{
				Name: "calendar-quota",
				Quota: &objects.APIKeyQuota{
					Requests: lo.ToPtr(int64(1)),
					Period: objects.APIKeyQuotaPeriod{
						Type:             objects.APIKeyQuotaPeriodTypeCalendarDuration,
						CalendarDuration: &objects.APIKeyQuotaCalendarDuration{Unit: unit},
					},
				},
			}})
			if unit == "year" {
				require.ErrorContains(t, err, "calendarDuration.unit is invalid")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
