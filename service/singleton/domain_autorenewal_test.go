package singleton

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAdvanceRenewalDate(t *testing.T) {
	now, _ := time.Parse("2006-01-02", "2026-08-31")

	// Case 1: 1 Year cycle, expired in 2026-07-11
	startStr, endStr, newEnd, renewed := advanceRenewalDate("2025-07-11", "2026-07-11", "Year", now)
	require.True(t, renewed)
	require.Equal(t, "2026-07-11", startStr)
	require.Equal(t, "2027-07-11", endStr)
	require.True(t, newEnd.After(now))

	// Case 2: 1 Month cycle, expired 3 months ago (2026-05-11)
	startStr, endStr, newEnd, renewed = advanceRenewalDate("2026-04-11", "2026-05-11", "Month", now)
	require.True(t, renewed)
	require.Equal(t, "2026-09-11", endStr)
	require.True(t, newEnd.After(now))

	// Case 3: RFC3339 format preserved
	startStr, endStr, newEnd, renewed = advanceRenewalDate("2025-07-11T00:00:00Z", "2026-07-11T00:00:00Z", "年", now)
	require.True(t, renewed)
	require.Contains(t, endStr, "2027-07-11")
	require.True(t, newEnd.After(now))

	// Case 4: Future date - should not renew
	_, _, _, renewed = advanceRenewalDate("2026-08-01", "2026-10-01", "Month", now)
	require.False(t, renewed)
}

func TestIsAutoRenewal(t *testing.T) {
	require.True(t, isAutoRenewal("1"))
	require.True(t, isAutoRenewal("true"))
	require.True(t, isAutoRenewal("auto"))
	require.True(t, isAutoRenewal("yes"))
	require.True(t, isAutoRenewal(true))
	require.True(t, isAutoRenewal(1))
	require.True(t, isAutoRenewal(float64(1)))

	require.False(t, isAutoRenewal("0"))
	require.False(t, isAutoRenewal("false"))
	require.False(t, isAutoRenewal(false))
	require.False(t, isAutoRenewal(0))
	require.False(t, isAutoRenewal(nil))
}
