package v1beta1

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestValidateVMAlertmanagerConfigFail(t *testing.T) {
	f := func(src string, expectedReason string) {
		t.Helper()
		var amc VMAlertmanagerConfig
		assert.NoError(t, json.Unmarshal([]byte(src), &amc))
		if len(amc.Status.ParsingSpecError) > 0 {
			if strings.Contains(amc.Status.ParsingSpecError, expectedReason) {
				return
			}
			t.Fatalf("unexpected parsing error: %s", amc.Status.ParsingSpecError)
		}
		err := amc.Validate()
		if assert.Error(t, err, "expect error:\n%s\n got nil", expectedReason) {
			assert.Contains(t, err.Error(), expectedReason)
		}
	}

	f(`
{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "test-fail"
    },
    "spec": {
        "receivers": [
            {
                "name": "blackhole"
            }
        ],
        "route": {
            "receiver": "blackhole",
            "routes": [
                {
                    "receiver": "blackhole",
                    "routes": [
                        {
                            "match": {
                                "key": "value"
                            },
                            "match_re":{
                                "key": "value.+"
                            }
                        }
                    ]
                }
            ]
        }
    }
}
`, `unknown object member name "match"`)
	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "test-fail"
    },
    "spec": {
        "receivers": [
            {
                "name": "blackhole"
            }
        ],
        "route": {
            "receiver": "non-exist",
            "routes": [
                {
                    "receiver": "blackhole",
                    "routes": [
                        {
                            "matchers": [
                                "nested=env",
                                "{\"foo\"!~\"[0-9]+\"}"
                            ]
                        }
                    ]
                }
            ]
        }
    }
}`, `receiver="non-exist" for spec root not found at receivers`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "test-fail"
    },
    "spec": {
        "receivers": [
            {
                "name": "blackhole"
            }
        ],
        "route": {
            "receiver": "blackhole",
            "routes": [
                {
                    "receiver": "blackhole",
                    "routes": [
                        {
                            "matchers": [
                                "nested=env"
                            ],
                            "receiver": "non-exist"
                        }
                    ]
                }
            ]
        }
    }
}`, `subRoute=0 is not valid: nested route=0: undefined receiver "non-exist" used in route`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "test-fail"
    },
    "spec": {
        "receivers": [
            {
                "name": "blackhole"
            }
        ],
        "route": {
            "routes": [
                {
                    "receiver": "blackhole",
                    "routes": [
                        {
                            "matchers": [
                                "nested=env"
                            ]
                        }
                    ]
                }
            ]
        }
    }
}`, `root route receiver cannot be empty`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "test-fail"
    },
    "spec": {
        "receivers": [
            {
                "name": "blackhole"
            }
        ],
        "route": {
            "receiver": "blackhole",
            "mute_time_intervals": [
                "daily"
            ],
            "routes": [
                {
                    "receiver": "blackhole",
                    "routes": [
                        {
                            "matchers": [
                                "nested=env"
                            ]
                        }
                    ]
                }
            ]
        }
    }
}`, `undefined mute time interval "daily" used in root route`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "test-fail"
    },
    "spec": {
        "receivers": [
            {
                "name": "blackhole"
            }
        ],
        "time_intervals": [
            {
                "name": "daily",
                "time_intervals": [
                    {
                        "months": [
                            "may:august"
                        ]
                    }
                ]
            }
        ],
        "route": {
            "receiver": "blackhole",
            "mute_time_intervals": [
                "months"
            ],
            "active_time_intervals": [
                "daily"
            ],
            "routes": [
                {
                    "receiver": "blackhole",
                    "routes": [
                        {
                            "matchers": [
                                "nested=env"
                            ]
                        }
                    ]
                }
            ]
        }
    }
}`, `undefined mute time interval "months" used in root route`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "test-fail"
    },
    "spec": {
        "receivers": [
            {
                "name": "blackhole"
            }
        ],
        "route": {
            "receiver": "blackhole",
            "matchers": [
                "bad !~-124 matcher\""
            ],
            "routes": [
                {
                    "receiver": "blackhole",
                    "routes": [
                        {
                            "matchers": [
                                "nested=env"
                            ]
                        }
                    ]
                }
            ]
        }
    }
}`, `expected a comma or close brace`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "jira"
    },
    "spec": {
        "receivers": [
            {
                "name": "jira",
                "jira_configs": [
                    {
                        "api_url": "https://dc-url",
                        "project": "some",
                        "labels": [
                            "dev",
                            "default"
                        ]
                    }
                ]
            }
        ],
        "route": {
            "receiver": "jira"
        }
    }
}`, `receivers[0]: jira_configs[0]: missing required field 'issue_type'`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "teamsv2"
    },
    "spec": {
        "receivers": [
            {
                "name": "teams",
                "msteamsv2_configs": [
                    {
                        "webhook_url_secret": {
                            "name": "ms-access",
                            "key": "SECRET"
                        },
                        "webhook_url": "http://example.com",
                        "http_config": {
                            "authorization": {
                                "credentials": {
                                    "name": "jira-access",
                                    "key": "DC_KEY"
                                }
                            }
                        },
                        "title": "some",
                        "text": "alert notification"
                    }
                ]
            }
        ],
        "route": {
            "receiver": "teams"
        }
    }
}`, `receivers[0]: msteamsv2_configs[0]: at most one of webhook_url or webhook_url_secret must be configured`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "teamsv2"
    },
    "spec": {
        "receivers": [
            {
                "name": "teams",
                "msteamsv2_configs": [
                    {
                        "http_config": {
                            "authorization": {
                                "credentials": {
                                    "name": "jira-access",
                                    "key": "DC_KEY"
                                }
                            }
                        },
                        "title": "some",
                        "text": "alert notification"
                    }
                ]
            }
        ],
        "route": {
            "receiver": "teams"
        }
    }
}`, `receivers[0]: msteamsv2_configs[0]: webhook_url or webhook_url_secret must be configured`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "teamsv2"
    },
    "spec": {
        "receivers": [
            {
                "name": "teams",
                "msteamsv2_configs": [
                    {
                        "http_config": {
                            "authorization": {
                                "credentials": {
                                    "name": "jira-access",
                                    "key": "DC_KEY"
                                }
                            },
                            "tls_config": {
                                "verify_certificate": true
                             }
                        },
                        "title": "some",
                        "text": "alert notification",
                        "webhook_url": "http://example.com"
                    }
                ]
            }
        ],
        "route": {
            "receiver": "teams"
        }
    }
}`, `unknown object member name "verify_certificate"`)
}

func TestValidateVMAlertmanagerConfigOk(t *testing.T) {
	f := func(src string) {
		t.Helper()
		var amc VMAlertmanagerConfig
		assert.NoError(t, json.Unmarshal([]byte(src), &amc))
		assert.Empty(t, amc.Status.ParsingSpecError)
		assert.NoError(t, amc.Validate())
	}
	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "slack"
    },
    "spec": {
        "receivers": [
            {
                "name": "slack",
                "slack_configs": [
                    {
                        "api_url": {
                            "key": "secret",
                            "name": "slack-hook"
                        },
                        "fields": [
                            {
                                "title": "some-title",
                                "value": "text"
                            }
                        ],
                        "actions": [
                            {
                                "type": "some",
                                "text": "template",
                                "name": "click",
                                "confirm": {
                                    "text": "button"
                                }
                            }
                        ]
                    }
                ]
            },
            {
                "name": "slack-2",
                "slack_configs": [
                    {
                        "api_url": {
                            "key": "secret",
                            "name": "slack-hook"
                        }
                    }
                ]
            }
        ],
        "time_intervals": [
            {
                "name": "daily",
                "time_intervals": [
                    {
                        "years": [
                            "2018:2025"
                        ]
                    }
                ]
            }
        ],
        "route": {
            "receiver": "slack",
            "active_time_intervals": [
                "daily"
            ],
            "routes": [
                {
                    "receiver": "slack-2",
                    "routes": [
                        {
                            "matchers": [
                                "nested=env"
                            ]
                        }
                    ]
                }
            ]
        }
    }
}`)
	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "email"
    },
    "spec": {
        "receivers": [
            {
                "name": "email",
                "email_configs": [
                    {
                        "require_tls": false,
                        "smarthost": "host:993",
                        "to": "some@email.com",
                        "from": "notification@example.com"
                    }
                ]
            }
        ],
        "time_intervals": [
            {
                "name": "daily",
                "time_intervals": [
                    {
                        "years": [
                            "2018:2025"
                        ]
                    }
                ]
            }
        ],
        "route": {
            "receiver": "email"
        }
    }
}`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "webhook"
    },
    "spec": {
        "time_intervals": [
            {
                "name": "dom-working",
                "time_intervals": [
                    {
                        "weekdays": [
                            "0:5"
                        ],
                        "times": [
                            {
                                "start_time": "08:00",
                                "end_time": "18:06"
                            }
                        ]
                    }
                ]
            },
            {
                "name": "dom-holidays",
                "time_intervals": [
                    {
                        "days_of_month": [
                            "3","1","5"
                        ]
                    }
                ]
            }
        ],
        "receivers": [
            {
                "name": "webhook",
                "webhook_configs": [
                    {
                        "url": "http://non-secret"
                    },
                    {
                        "url_secret": {
                            "name": "hook",
                            "key": "secret_url"
                        }
                    }
                ]
            }
        ],
        "route": {
            "receiver": "webhook",
            "routes": [
                {
                    "matchers": [
                        "team=\"week\""
                    ],
                    "receiver": "webhook",
                    "mute_time_intervals": [
                        "dom-working"
                    ]
                },
                {
                    "matchers": [
                        "team=\"daily\""
                    ],
                    "receiver": "webhook",
                    "active_time_intervals": [
                        "dom-working"
                    ]
                }
            ]
        }
    }
}`)
	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "webex"
    },
    "spec": {
        "receivers": [
            {
                "name": "webex",
                "webex_configs": [
                    {
                        "room_id": "dev-team",
                        "http_config": {
                            "authorization": {
                                "credentials": {
                                    "key": "WEBEX_SECRET",
                                    "name": "webex-access"
                                }
                            }
                        }
                    }
                ]
            }
        ],
        "route": {
            "receiver": "webex"
        }
    }
}`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "msteams"
    },
    "spec": {
        "receivers": [
            {
                "name": "teams",
                "msteams_configs": [
                    {
                        "webhook_url": "https://open-for-all.example"
                    },
                    {
                        "webhook_url_secret": {
                            "name": "teams-access",
                            "key": "secret-url"
                        }
                    }
                ]
            }
        ],
        "route": {
            "receiver": "teams"
        }
    }
}`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "sns"
    },
    "spec": {
        "receivers": [
            {
                "name": "sns-arn",
                "sns_configs": [
                    {
                        "target_arn": "some"
                    },
                    {
                        "topic_arn": "topic"
                    }
                ]
            },
            {
                "name": "sns-phone",
                "sns_configs": [
                    {
                        "phone_number": "1234",
                        "sigv4": {
                            "region": "eu-west-1",
                            "profile": "dev"
                        }
                    }
                ]
            }
        ],
        "route": {
            "receiver": "sns-arn",
            "routes": [
                {
                    "matchers": [
                        "test=\"team\"",
                        "type=\"phone\"",
                        "{\"cloud.account.name\"=\"prod\"}"
                    ],
                    "receiver": "sns-phone"
                }
            ]
        }
    }
}`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "discord"
    },
    "spec": {
        "receivers": [
            {
                "name": "ds",
                "discord_configs": [
                    {
                        "webhook_url": "https://open-for-all.example"
                    },
                    {
                        "webhook_url_secret": {
                            "name": "ds-access",
                            "key": "SECRET_URL"
                        }
                    }
                ]
            }
        ],
        "route": {
            "receiver": "ds"
        }
    }
}`)
	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "pushover"
    },
    "spec": {
        "receivers": [
            {
                "name": "po",
                "pushover_configs": [
                    {
                        "token": {
                            "name": "po-access",
                            "key": "TOKEN"
                        },
                        "user_key": {
                            "name": "po-access",
                            "key": "USER_KEY"
                        }
                    }
                ]
            }
        ],
        "route": {
            "receiver": "po"
        }
    }
}`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "victorops"
    },
    "spec": {
        "receivers": [
            {
                "name": "vo",
                "victorops_configs": [
                    {
                        "api_key": {
                            "name": "vo-access",
                            "key": "SECRET_URL"
                        },
                        "routing_key": "CRITICAL",
                        "custom_fields": {
                            "key": "value"
                        }
                    }
                ]
            }
        ],
        "route": {
            "receiver": "vo"
        }
    }
}`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "msteams"
    },
    "spec": {
        "receivers": [
            {
                "name": "wc",
                "msteams_configs": [
                    {
                        "webhook_url": "https://open-for-all.example"
                    },
                    {
                        "webhook_url_secret": {
                            "name": "teams-access",
                            "key": "secret-url"
                        }
                    }
                ]
            }
        ],
        "route": {
            "receiver": "wc"
        }
    }
}`)
	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "pagerduty"
    },
    "spec": {
        "receivers": [
            {
                "name": "pd",
                "pagerduty_configs": [
                    {
                        "routing_key": {
                            "name": "pd-access",
                            "key": "secret"
                        }
                    },
                    {
                        "service_key": {
                            "name": "pd-access",
                            "key": "secret"
                        }
                    }
                ]
            }
        ],
        "route": {
            "receiver": "pd"
        }
    }
}`)
	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "telegram"
    },
    "spec": {
        "receivers": [
            {
                "name": "tg",
                "telegram_configs": [
                    {
                        "bot_token": {
                            "name": "tg-access",
                            "key": "secret"
                        },
                        "chat_id": 1234,
                        "parse_mode": "HTML",
                        "message_thread_id": 15
                    }
                ]
            }
        ],
        "route": {
            "receiver": "tg"
        }
    }
}`)
	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "jira"
    },
    "spec": {
        "receivers": [
            {
                "name": "jira",
                "jira_configs": [
                    {
                        "api_url": "https://dc-url",
                        "http_config": {
                            "authorization": {
                                "credentials": {
                                    "name": "jira-access",
                                    "key": "DC_KEY"
                                }
                            }
                        },
                        "project": "some",
                        "issue_type": "BUG",
                        "labels": [
                            "dev",
                            "default"
                        ]
                    }
                ]
            }
        ],
        "route": {
            "receiver": "jira"
        }
    }
}`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "teamsv2"
    },
    "spec": {
        "receivers": [
            {
                "name": "teams",
                "msteamsv2_configs": [
                    {
                        "webhook_url": "https://dc-url",
                        "http_config": {
                            "authorization": {
                                "credentials": {
                                    "name": "jira-access",
                                    "key": "DC_KEY"
                                }
                            }
                        },
                        "title": "some",
                        "text": "alert notification"
                    }
                ]
            }
        ],
        "route": {
            "receiver": "teams"
        }
    }
}`)

	f(`{
    "apiVersion": "v1",
    "kind": "VMAlertmanagerConfig",
    "metadata": {
        "name": "teamsv2"
    },
    "spec": {
        "receivers": [
            {
                "name": "teams",
                "msteamsv2_configs": [
                    {
                        "webhook_url_secret": {
                            "name": "ms-access",
                            "key": "SECRET"
                        },
                        "http_config": {
                            "authorization": {
                                "credentials": {
                                    "name": "jira-access",
                                    "key": "DC_KEY"
                                }
                            }
                        },
                        "title": "some",
                        "text": "alert notification"
                    }
                ]
            }
        ],
        "route": {
            "receiver": "teams"
        }
    }
}`)
}

// TestVMAlertmanagerConfigCaseIgnore verifies that both snake_case and camelCase
// field names are accepted when unmarshalling VMAlertmanagerConfig, thanks to the
// json "case:ignore" tag option processed by encoding/json/v2.
func TestVMAlertmanagerConfigCaseIgnore(t *testing.T) {
	unmarshal := func(t *testing.T, src string) VMAlertmanagerConfig {
		t.Helper()
		var amc VMAlertmanagerConfig
		assert.NoError(t, json.Unmarshal([]byte(src), &amc))
		assert.Empty(t, amc.Status.ParsingSpecError)
		return amc
	}

	t.Run("spec top-level fields camelCase", func(t *testing.T) {
		amc := unmarshal(t, `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMAlertmanagerConfig",
			"metadata": {"name": "test"},
			"spec": {
				"receivers": [{"name": "recv"}],
				"inhibitRules": [
					{
						"targetMatchers": ["env=prod"],
						"sourceMatchers": ["env=dev"],
						"equal": ["alertname"]
					}
				],
				"timeIntervals": [
					{
						"name": "workhours",
						"timeIntervals": [
							{
								"daysOfMonth": ["1:5"],
								"times": [{"startTime": "09:00", "endTime": "17:00"}]
							}
						]
					}
				],
				"route": {"receiver": "recv"}
			}
		}`)
		assert.Len(t, amc.Spec.InhibitRules, 1)
		assert.Equal(t, []string{"env=prod"}, amc.Spec.InhibitRules[0].TargetMatchers)
		assert.Equal(t, []string{"env=dev"}, amc.Spec.InhibitRules[0].SourceMatchers)
		assert.Len(t, amc.Spec.TimeIntervals, 1)
		assert.Equal(t, "workhours", amc.Spec.TimeIntervals[0].Name)
		ti := amc.Spec.TimeIntervals[0].TimeIntervals[0]
		assert.Equal(t, []string{"1:5"}, ti.DaysOfMonth)
		assert.Equal(t, "09:00", ti.Times[0].StartTime)
		assert.Equal(t, "17:00", ti.Times[0].EndTime)
	})

	t.Run("route camelCase fields", func(t *testing.T) {
		amc := unmarshal(t, `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMAlertmanagerConfig",
			"metadata": {"name": "test"},
			"spec": {
				"receivers": [{"name": "recv"}, {"name": "recv2"}],
				"timeIntervals": [{"name": "quiet", "timeIntervals": [{"weekdays": ["saturday", "sunday"]}]}],
				"route": {
					"receiver": "recv",
					"groupBy": ["alertname", "cluster"],
					"groupWait": "30s",
					"groupInterval": "5m",
					"repeatInterval": "12h",
					"muteTimeIntervals": ["quiet"],
					"activeTimeIntervals": ["quiet"],
					"routes": [{"receiver": "recv2"}]
				}
			}
		}`)
		r := amc.Spec.Route
		assert.Equal(t, []string{"alertname", "cluster"}, r.GroupBy)
		assert.Equal(t, "30s", r.GroupWait)
		assert.Equal(t, "5m", r.GroupInterval)
		assert.Equal(t, "12h", r.RepeatInterval)
		assert.Equal(t, []string{"quiet"}, r.MuteTimeIntervals)
		assert.Equal(t, []string{"quiet"}, r.ActiveTimeIntervals)
	})

	t.Run("nested route camelCase fields", func(t *testing.T) {
		amc := unmarshal(t, `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMAlertmanagerConfig",
			"metadata": {"name": "test"},
			"spec": {
				"receivers": [{"name": "recv"}],
				"route": {
					"receiver": "recv",
					"routes": [
						{
							"receiver": "recv",
							"groupWait": "10s",
							"groupInterval": "2m",
							"matchers": ["env=prod"]
						}
					]
				}
			}
		}`)
		assert.Len(t, amc.Spec.Route.Routes, 1)
		sub := amc.Spec.Route.Routes[0]
		assert.Equal(t, "10s", sub.GroupWait)
		assert.Equal(t, "2m", sub.GroupInterval)
	})

	t.Run("receiver list fields camelCase", func(t *testing.T) {
		amc := unmarshal(t, `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMAlertmanagerConfig",
			"metadata": {"name": "test"},
			"spec": {
				"receivers": [
					{
						"name": "recv",
						"webhookConfigs": [
							{
								"url": "http://example.com/hook",
								"sendResolved": true,
								"maxAlerts": 5
							}
						],
						"telegramConfigs": [
							{
								"botToken": {"name": "secret", "key": "token"},
								"chatId": 12345,
								"sendResolved": false
							}
						]
					}
				],
				"route": {"receiver": "recv"}
			}
		}`)
		recv := amc.Spec.Receivers[0]
		assert.Len(t, recv.WebhookConfigs, 1)
		wh := recv.WebhookConfigs[0]
		assert.Equal(t, "http://example.com/hook", *wh.URL)
		assert.Equal(t, true, *wh.SendResolved)
		assert.Equal(t, int32(5), wh.MaxAlerts)

		assert.Len(t, recv.TelegramConfigs, 1)
		tg := recv.TelegramConfigs[0]
		assert.Equal(t, corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "secret"}, Key: "token"}, *tg.BotToken)
		assert.Equal(t, 12345, tg.ChatID)
		assert.Equal(t, false, *tg.SendResolved)
	})

	t.Run("http_config camelCase fields", func(t *testing.T) {
		amc := unmarshal(t, `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMAlertmanagerConfig",
			"metadata": {"name": "test"},
			"spec": {
				"receivers": [
					{
						"name": "recv",
						"webhookConfigs": [
							{
								"url": "http://example.com/hook",
								"httpConfig": {
									"bearerTokenSecret": {"name": "secret", "key": "token"},
									"followRedirects": false,
									"tls_config": {
										"insecure_skip_verify": true,
										"server_name": "example.com"
									}
								}
							}
						]
					}
				],
				"route": {"receiver": "recv"}
			}
		}`)
		hc := amc.Spec.Receivers[0].WebhookConfigs[0].HTTPConfig
		assert.Equal(t, "secret", hc.BearerTokenSecret.Name)
		assert.Equal(t, "token", hc.BearerTokenSecret.Key)
		assert.Equal(t, false, *hc.FollowRedirects)
		assert.Equal(t, true, hc.TLSConfig.InsecureSkipVerify)
		assert.Equal(t, "example.com", hc.TLSConfig.ServerName)
	})

	t.Run("ProxyConfig snake_case for camelCase canonical fields", func(t *testing.T) {
		amc := unmarshal(t, `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMAlertmanagerConfig",
			"metadata": {"name": "test"},
			"spec": {
				"receivers": [
					{
						"name": "recv",
						"webhookConfigs": [
							{
								"url": "http://example.com/hook",
								"httpConfig": {
									"proxy_url": "http://proxy:3128",
									"no_proxy": "localhost,127.0.0.1"
								}
							}
						]
					}
				],
				"route": {"receiver": "recv"}
			}
		}`)
		hc := amc.Spec.Receivers[0].WebhookConfigs[0].HTTPConfig
		assert.Equal(t, "http://proxy:3128", hc.ProxyURL)
		assert.Equal(t, "localhost,127.0.0.1", hc.NoProxy)
	})

	t.Run("snake_case canonical fields still work (regression)", func(t *testing.T) {
		amc := unmarshal(t, `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMAlertmanagerConfig",
			"metadata": {"name": "test"},
			"spec": {
				"receivers": [{"name": "recv"}],
				"inhibit_rules": [
					{
						"target_matchers": ["env=prod"],
						"source_matchers": ["env=dev"]
					}
				],
				"route": {
					"receiver": "recv",
					"group_by": ["alertname"],
					"group_wait": "30s",
					"group_interval": "5m",
					"repeat_interval": "12h"
				}
			}
		}`)
		assert.Len(t, amc.Spec.InhibitRules, 1)
		assert.Equal(t, "30s", amc.Spec.Route.GroupWait)
		assert.Equal(t, []string{"alertname"}, amc.Spec.Route.GroupBy)
	})

	t.Run("mixed snake_case and camelCase in same object", func(t *testing.T) {
		amc := unmarshal(t, `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMAlertmanagerConfig",
			"metadata": {"name": "test"},
			"spec": {
				"receivers": [{"name": "recv"}],
				"route": {
					"receiver": "recv",
					"group_wait": "30s",
					"groupInterval": "5m",
					"repeat_interval": "12h"
				}
			}
		}`)
		assert.Equal(t, "30s", amc.Spec.Route.GroupWait)
		assert.Equal(t, "5m", amc.Spec.Route.GroupInterval)
		assert.Equal(t, "12h", amc.Spec.Route.RepeatInterval)
	})
}
