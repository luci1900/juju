// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package sshproxy

//go:generate go run github.com/canonical/gomock/mockgen -package sshproxy -destination metrics_mock_test.go github.com/juju/juju/apiserver/sshproxy MetricsCollector
