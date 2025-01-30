/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("VMAlertmanagerConfig Webhook", func() {
	Context("When creating VMAlertmanagerConfig under Validating Webhook", func() {
		DescribeTable("fail validation",
			func(srcYAML string, wantErrText string) {
				var amc VMAlertmanagerConfig
				Expect(yaml.Unmarshal([]byte(srcYAML), &amc)).To(Succeed())
				cfgJSON, err := json.Marshal(amc)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(json.Unmarshal(cfgJSON, &amc)).ShouldNot(HaveOccurred())
				Expect(amc.Validate()).To(MatchError(wantErrText))
			},
			Entry("non-exist receiver at root route", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: test-fail
        spec:
          receivers:
          - name: blackhole
          route:
            receiver: non-exist
            routes:
            - receiver: blackhole
              routes:
              - matcher: [nested=env]
        `, `receiver="non-exist" for spec root not found at receivers`),
			Entry("non-exist receiver at nested routes", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: test-fail
        spec:
          receivers:
          - name: blackhole
          route:
            receiver: blackhole
            routes:
            - receiver: blackhole
              routes:
              - matcher: [nested=env]
                receiver: non-exist
        `, `subRoute=0 is not valid: nested route=0: undefined receiver "non-exist" used in route`),
			Entry("missing receiver at root", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: test-fail
        spec:
          receivers:
          - name: blackhole
          route:
            routes:
            - receiver: blackhole
              routes:
              - matcher: [nested=env]
        `, `root route reciever cannot be empty`),
			Entry("missing mute interval", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: test-fail
        spec:
          receivers:
          - name: blackhole
          route:
            receiver: blackhole 
            mute_time_intervals:
            - daily
            routes:
            - receiver: blackhole
              routes:
              - matcher: [nested=env]
        `, `undefined mute time interval "daily" used in root route`),
			Entry("missing active time interval", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: test-fail
        spec:
          receivers:
          - name: blackhole
          time_intervals:
          - name: daily
            time_intervals:
            - months: [may:august]
          route:
            receiver: blackhole 
            mute_time_intervals:
            - months
            active_time_intervals:
            - daily
            routes:
            - receiver: blackhole
              routes:
              - matcher: [nested=env]
        `, `undefined mute time interval "months" used in root route`),
			Entry("incorrect matchers syntax", `
        apiVersion: v1
        kind: VMAlertmanagerConfig
        metadata:
          name: test-fail
        spec:
          receivers:
          - name: blackhole
          route:
            receiver: blackhole
            matchers:
            - bad !~-124 matcher"
            routes:
            - receiver: blackhole
              routes:
              - matcher: [nested=env]
        `, `cannot parse nested route for alertmanager config err: cannot parse matchers="bad !~-124 matcher\"" idx=0 for route_receiver=blackhole: matcher value contains unescaped double quote: -124 matcher"`),
		)
		DescribeTable("should pass validation",
			func(srcYAML string) {
				var amc VMAlertmanagerConfig
				Expect(yaml.Unmarshal([]byte(srcYAML), &amc)).To(Succeed())
				cfgJSON, err := json.Marshal(amc)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(json.Unmarshal(cfgJSON, &amc)).ShouldNot(HaveOccurred())
				Expect(amc.Validate()).To(Succeed())
			},
			Entry("slack", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: slack
        spec:
          receivers:
          - name: slack
            slack_configs:
            - api_url:
               key: secret
               name: slack-hook
              fields:
              - title: some-title
                value: text
              actions:
              - type: some
                text: template
                name: click
                confirm:
                  text: button
              
          - name: slack-2
            slack_configs:
            - api_url:
               key: secret
               name: slack-hook

          time_intervals:
          - name: daily
            time_intervals:
            - years: [2018:2025]
          route:
            receiver: slack
            active_time_intervals:
            - daily
            routes:
            - receiver: slack-2
              routes:
              - matcher: [nested=env]

        `),
			Entry("email", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: email
        spec:
          receivers:
          - name: email
            email_configs:
            - require_tls: false
              smarthost: host:993
              to: some@email.com
              from: notification@example.com

          time_intervals:
          - name: daily
            time_intervals:
            - years: [2018:2025]
          route:
            receiver: email
        `),
			Entry("webhook", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: webhook
        spec:
          time_intervals:
          - name: dom-working
            time_intervals:
            - weekdays: [0:5]
              times: 
              - start_time: 08:00
                end_time: 18:00
          - name: dom-holidays
            time_intervals:
            - days_of_month: [5:15]
          receivers:
          - name: webhook
            webhook_configs:
            - url: "http://non-secret"
            - url_secret:
                name: hook
                key: secret_url
          route:
            receiver: webhook
            routes:
            - matcher:
              - team="week"
              receiver: webhook
              mute_time_intervals:
              - dom-working
            - matcher:
              - team="daily"
              receiver: webhook
              active_time_intervals:
              - dom-working
        `),
			Entry("webex", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: webex
        spec:
          receivers:
          - name: webex
            webex_configs:
            - room_id: dev-team
              http_config:
               authorization:
                 credentials:
                   key: WEBEX_SECRET
                   name: webex-access
          route:
            receiver: webex

        `),
			Entry("msteams", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: msteams
        spec:
          receivers:
          - name: teams
            msteams_configs:
            - webhook_url: "https://open-for-all.example"
            - webhook_url_secret: 
                name: teams-access
                key: secret-url
    
          route:
            receiver: teams

        `),
			Entry("sns", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: sns
        spec:
          receivers:
          - name: sns-arn
            sns_configs:
            - target_arn: "some"
            - topic_arn: "topic"
          - name: sns-phone
            sns_configs:
            - phone_number: "1234"
              sigv4:
                region: eu-west-1
                profile: dev
          route:
            receiver: sns-arn
            routes:
            - matcher:
              - test="team"
              - type="phone"
              receiver: sns-phone 

        `),
			Entry("discord", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: discord
        spec:
          receivers:
          - name: ds
            discord_configs:
            - webhook_url: "https://open-for-all.example"
            - webhook_url_secret: 
                name: ds-access
                key: SECRET_URL
    
          route:
            receiver: ds

        `),
			Entry("pushover", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: pushover
        spec:
          receivers:
          - name: po
            pushover_configs:
            - token: 
                name: po-access
                key: TOKEN
              user_key:
                name: po-access
                key: USER_KEY

          route:
            receiver: po

        `),
			Entry("victorops", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: victorops
        spec:
          receivers:
          - name: vo
            victorops_configs:
            - api_key: 
                name: vo-access
                key: SECRET_URL
              routing_key: CRITICAL
    
          route:
            receiver: vo

        `),
			Entry("wechat", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: wechat
        spec:
          receivers:
          - name: wc
            wechat_configs:
            - webhook_url: "https://open-for-all.example"
            - webhook_url_secret: 
                name: teams-access
                key: secret-url
    
          route:
            receiver: wc

        `),
			Entry("pagerduty", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: pagerduty
        spec:
          receivers:
          - name: pd
            pagerduty_configs:
            - routing_key:
                name: pd-access
                key: secret
              custom_fields:
                key: value

            - service_key: 
                name: pd-access
                key: secret
          route:
            receiver: pd
        `),
			Entry("telegram", `
        apiVersion: v1 
        kind: VMAlertmanagerConfig
        metadata:
          name: telegram
        spec:
          receivers:
          - name: tg
            telegram_configs:
            - bot_token:
                name: tg-access
                key: secret
              chat_id: 1234
              parse_mode: HTML
              message_thread_id: 15
          route:
            receiver: tg
        `),
		)
	})

	Context("When creating VMAlertmanagerConfig under Conversion Webhook", func() {
		It("Should get the converted version of VMAlertmanagerConfig", func() {
			// TODO(user): Add your logic here
		})
	})
})
