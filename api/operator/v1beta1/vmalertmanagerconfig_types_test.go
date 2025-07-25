package v1beta1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateVMAlertmanagerConfigFail(t *testing.T) {
	f := func(src string, expectedReason string) {
		t.Helper()
		var amc VMAlertmanagerConfig
		if err := json.Unmarshal([]byte(src), &amc); err != nil {
			t.Fatalf("unexpected json parse error: %s", err)
		}
		if len(amc.Spec.ParsingError) > 0 {
			errS := amc.Spec.ParsingError
			if strings.Contains(errS, expectedReason) {
				return
			}
			t.Fatalf("unexpected parsing error: %s", amc.Spec.ParsingError)
		}
		err := amc.Validate()
		if err == nil {
			t.Fatalf("expect error:\n%s\n got nil", expectedReason)
		}
		if !strings.Contains(err.Error(), expectedReason) {
			t.Fatalf("unexpected fail reason:\n%s\nmust contain:\n%s\n", err.Error(), expectedReason)
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
`, `unknown field "match"`)
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
}`, `receiver at idx=0 is invalid: at idx=0 for jira_configs missing required field 'issue_type'`)

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
}`, `receiver at idx=0 is invalid: at idx=0 for msteamsv2_configs at most one of webhook_url or webhook_url_secret must be configured`)

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
}`, `receiver at idx=0 is invalid: at idx=0 for msteamsv2_configs of webhook_url or webhook_url_secret must be configured`)

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
                                "insecure_skip_verify": true
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
}`, `unknown field "insecure_skip_verify"`)
}

func TestValidateVMAlertmanagerConfigOk(t *testing.T) {
	f := func(src string) {
		t.Helper()
		var amc VMAlertmanagerConfig
		if err := json.Unmarshal([]byte(src), &amc); err != nil {
			t.Fatalf("unexpected json parse error: %s", err)
		}
		if len(amc.Spec.ParsingError) > 0 {
			t.Fatalf("unexpected parsing error: %s", amc.Spec.ParsingError)
		}
		err := amc.Validate()
		if err != nil {
			t.Fatalf("expect error:\n%s", err)
		}
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
                        "routing_key": "CRITICAL"
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
        "name": "wechat"
    },
    "spec": {
        "receivers": [
            {
                "name": "wc",
                "wechat_configs": [
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
                        },
                        "custom_fields": {
                            "key": "value"
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
