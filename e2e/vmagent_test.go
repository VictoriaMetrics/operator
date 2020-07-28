package e2e

import (
	operator "github.com/VictoriaMetrics/operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

var _ = Describe("test  vmagent Controller", func() {
	Context("e2e ", func() {
		Context("crud", func() {
			Context("create", func() {
				name := "create-vma"
				namespace := "default"
				AfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &operator.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
					})).To(BeNil())
				})
				It("should create vmagent", func() {
					Expect(k8sClient.Create(context.TODO(), &operator.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
						Spec: operator.VMAgentSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							RemoteWrite: []operator.VMAgentRemoteWriteSpec{
								{URL: "http://localhost:8428"},
							},
						}})).To(BeNil())
					currVMAgent := &operator.VMAgent{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, currVMAgent)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, currVMAgent.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
				})
				It("should create vmagent with tls remote target", func() {
					tlsSecretName := "vmagent-remote-tls"
					tlsSecret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      tlsSecretName,
							Namespace: namespace,
						},
						StringData: map[string]string{
							"remote-ca":   tlsCA,
							"remote-cert": tlsCert,
							"remote-key":  tlsKey,
						},
					}
					Expect(k8sClient.Create(context.TODO(), tlsSecret)).To(Succeed())
					Expect(k8sClient.Create(context.TODO(), &operator.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
						Spec: operator.VMAgentSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							RemoteWrite: []operator.VMAgentRemoteWriteSpec{
								{URL: "http://localhost:8428"},
								{
									URL: "http://localhost:8425",
									TLSConfig: &operator.TLSConfig{
										CA: operator.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: tlsSecretName,
												},
												Key: "remote-ca",
											},
										},
										Cert: operator.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: tlsSecretName,
												},
												Key: "remote-cert",
											},
										},
										KeySecret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecretName,
											},
											Key: "remote-key",
										},
									},
								},
							},
						}})).To(BeNil())
					currVMAgent := &operator.VMAgent{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, currVMAgent)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, currVMAgent.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
					Expect(k8sClient.Delete(context.TODO(), tlsSecret)).To(Succeed())
				})

			})
			Context("update", func() {
				name := "update-vma"
				namespace := "default"
				JustAfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &operator.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						}})).To(BeNil())

				})
				JustBeforeEach(func() {
					Expect(k8sClient.Create(context.TODO(), &operator.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
						Spec: operator.VMAgentSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							RemoteWrite: []operator.VMAgentRemoteWriteSpec{
								{URL: "http://some-vm-single:8428"},
							},
						},
					})).To(BeNil())

				})
				It("should expand vmagent up to 3 replicas", func() {
					currVMAgent := &operator.VMAgent{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, currVMAgent)).To(BeNil())
					currVMAgent.Spec.ReplicaCount = pointer.Int32Ptr(3)
					Expect(k8sClient.Update(context.TODO(), currVMAgent))
					Eventually(func() string {
						return expectPodCount(k8sClient, 3, namespace, currVMAgent.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
				})
			})
		})

	})
})

var (
	tlsCA = `-----BEGIN CERTIFICATE-----
MIIFfTCCA2WgAwIBAgIUBNFQu/Q4hyC6zm6QTBxh63+I/lUwDQYJKoZIhvcNAQEL
BQAwTjELMAkGA1UEBhMCVEUxCzAJBgNVBAgMAlRFMQswCQYDVQQHDAJURTELMAkG
A1UECgwCVEUxCzAJBgNVBAsMAlRFMQswCQYDVQQDDAJURTAeFw0yMDA3MjgwODQ0
MjJaFw0yMTA3MjgwODQ0MjJaME4xCzAJBgNVBAYTAlRFMQswCQYDVQQIDAJURTEL
MAkGA1UEBwwCVEUxCzAJBgNVBAoMAlRFMQswCQYDVQQLDAJURTELMAkGA1UEAwwC
VEUwggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQCpX9JifNTY0lyItiTI
DEDJK1kr4rOKBGIDqfR5GRn8IGwkHicif/FqOreHPpPfwN6nT99QYjp7uZGRtUgL
2T7szu/PcAvOSb7R3QKenB4Xh6f6brlI4qPQdaarGEQbLIqgq4hV7XuYG4pMEb68
G4amTK2WPKaOIFFH72eNxfOe4TTU/J55ItKlGMUK4CwBYQe2bQOFSrvxzDmCqt1d
75HrJTDnWWp8gpPn9yCsTGW4K/4c0nwZPcm2vBcg41ByE4mJxGEdV0XaU7RxLups
eY6fRa10dYFiU9IGZcLbod0neaclQvS0z4EQBR7Xj6MnLrRJ5dpExaksoMLy/1VM
dfV7/tBFNmKf+fi7mKFA/BltEN1XBEhSUgA8FZq/XpS0/7ULWNcQ2AVc8Selosz8
5w6k6R10bsSe2Ttysi2oR2Y+P1JR74G2wz1hxNiEDPEySRoUo1UZSZODZD9smqYc
rR+QEmH+wtDfQR4pXkyZyaG8Y5qXYKNUMaJH8NPvjw8uWttVoGD1IcmF3mZwVwSU
K4aH7BG09qo4/MijseYMcWWXA0vgCBj8sXXgzDwXZyowiwWrpOriHpNUpiiR8NeT
9lFaHiYxuPj5MjZrGWCxXcXgtWaAj9X9B6Kv0jCA8PhYgO0FkCi2cJEOHFJc7FxO
EfjDoFg4wHd4DVp7z9pSBHe4zwIDAQABo1MwUTAdBgNVHQ4EFgQUkGcr4XZ8GC2W
s18CTuvCtM3pwckwHwYDVR0jBBgwFoAUkGcr4XZ8GC2Ws18CTuvCtM3pwckwDwYD
VR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAgEAN+H+244JARmkjYBtZspy
75IAtKH2rYhbXLB57YP8BNYjSST/HGn7kVAhFbEs4YpHbrK2jiXnMjEKBv1ZCqEv
Q+2DpTf4tKz3QBwZ2zph9ET/WfWZI2Magm6q0IEVhdNID595EdeT+RyT5ccyXQ30
+jgLBd4pAWY0xiTlL9udpYxV7MQdG/7Lk/t1sUgKUUQNgX3kuIs9HjCj12o+Dx2y
uYt2TYRk0oWSXR6JBFX3XiLOuQa89olmUDj67AFTQyE0hZcsWvboxYPNT68kPuiA
Gk9XVpbt4fmAs55SfGA31QDkGNqYBBCCxFJIHQ+lleNdR3862wYXCh+Vgat6452Q
VFhoyJp2SDqwqXSgB4zbkTfLF+Wn1uEBfZoFthjC1hD1j9+g7dh+Dbs03RJF6Bvl
K1S9jDNVfP5Mw4+JZnLWqlCjxCMXW96YgnGzi0T82ntP7dBMofj3DjbNeRJwgk7q
0gD55CiufmpTfLT2NUmSUAmG1MTkodXTEX7XRIyiF8eqhL2iefoc6zuh3KRj4oSv
i7TjcCMJ3XEQ31zOPjTd1SkCiBvNS6sh9JC0NFK0i5Ju+53MjNJ734Km4kpXKoaJ
tIP7kt7mbXWdcZQkznBwArupOUWZEMw3gm0J3mLDJ6ifJieHIb/wSsurh2/tMrqB
MY0V6Pd9E788PphPk/eIaI4=
-----END CERTIFICATE-----`
	tlsKey = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDRtQ6WsCmYdwOx
j58F2DmaaVd123IflPRwei9tmuv8kjiN24gT0PBbdL2NX8vPR8szQNUikeCMd5Jy
QenVueLQRG796ujjMz7Srcdl5kiCa+AN82GYZxgD2GmL2wuJvq0cXgzGQHSCv55f
WojNojGAjwr/yZcS7JCE8w3MfyBsRu5s5fqWQjFhF8suSVXSz3eg+PofILrnrN6x
g4uJIpZYEmXXlaL3DV+tIqVsC2tROsDUrEkg2dx1vuNFa7DJSthpLKR3A3bZik7+
1KzSHnIH/rCn+lIjR1ifcZ1RozdCMsaRfDFe8jQmX4hvSvTs66T9q+2jLp4zy/1J
JbeiFUGNAgMBAAECggEAOvQjfclYaDxNFYXCtunqh7ZFmCRxGN/POC+hVbbP0Nlq
fLbSsn9yksNm5m+f5E3Smj4HrQhFkDetO+G70xHG6bXTXh7ECdtGNgQUoljy2Xdq
LYHWVfnljm8wfNi/jaHFGMx32uQT3Q3xf+z7uJN4RyPve6k4h2Fp33ZU0sCKZOWp
qnE9e0vNPg0OZe3cPISiGHInPCjZn8ngkps1QDnO+rt2qKWl1Ph13pXMnMz40+u2
oHNNS9XiYL83GuU4ZcrnEpFoAZA5YvXSO5/r90b0SyAyBYRnHLzGsDsYdHnSwT/X
4T1oCftqUUb1g/x4VujzJGgQJZkwfcrGcADEnOWaAQKBgQDpNSpjwI6eZLRuTcO/
u10QN+8nLWmMxXJa5BRoy/5qCcgf9uvq9GU1x73t6er1pr0mwUB25Q+QGUMq4LAm
D8OumxcPxftvuwZaGW3c8In8pzwCAi01MHG/+2oMHN1WTV2jlzWvQtU75Ykwxs32
8L+7wNqmm38ogWuTS6zsbjOP/QKBgQDmM+m4rONdfhRERPNhs9YFoXLDS08N//Fd
8izi/y7a/Dh315lmRZk6lU7GJSzxQ0WKygd5wz/RjJfYfT0bonjeB4wKeqvV8Ujb
3Xq3GpBoO3WwPFU4DjyX53p/F9cCCgckhNFUGSzOR+8JXG17GoAHu2Iqfpt6RLpk
8wiuVWvE0QKBgB9wRmWqOM/LnbNdEm2Pka01DS2H5rnOiGsOYl36WjLrXKpKfGVx
Sw+j/MvNBBrXvpox5UHiAWYYscBfCAApkeTBDavXsdzPJr0QvonRd5iy5tkSeAu6
mysZdqNpZMFUrrH2GYumA98OQ59qvatzqzVhe1iIj+zi/aCezBIXjSX1AoGBALCz
9Jofi79+QgxNaQz8QDK+RRuHuT0j06Crfq0X+F178dR8GHIaxo3jgj4y1xay7rSk
c6yRpXEynHQ/XiLSSjkUTfjVRQXKWoT6s3HN4D9CNQp8pWWL+BMaSjs4j4AvNmBf
21bUpEILkX78BcXTB6fnvGimGq52ByXqMCWxyDGhAoGALxgtAo9aIDffmmnszy/2
Nu2nxiA+RVYiH80z3vxQI/oewb4FVTAtJ6A1rYGgbAs5icHQ564xnsjAxH6UeGTU
4PSHnceAZ1MxXQirdOYIg9HPhl+0JBs+KixGclTWWWERtcLbv/UtMZzRKNlair05
Berg9dOTyoXzr1BvqKq7PUw=
-----END PRIVATE KEY-----
`
	tlsCert = `-----BEGIN CERTIFICATE-----
MIIEIzCCAgsCFEuh9p7lgotaef4oqeHZhSOVgmKWMA0GCSqGSIb3DQEBCwUAME4x
CzAJBgNVBAYTAlRFMQswCQYDVQQIDAJURTELMAkGA1UEBwwCVEUxCzAJBgNVBAoM
AlRFMQswCQYDVQQLDAJURTELMAkGA1UEAwwCVEUwHhcNMjAwNzI4MDg0NzAwWhcN
MjAwOTI2MDg0NzAwWjBOMQswCQYDVQQGEwJURTELMAkGA1UECAwCVEUxCzAJBgNV
BAcMAlRFMQswCQYDVQQKDAJURTELMAkGA1UECwwCVEUxCzAJBgNVBAMMAlRFMIIB
IjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0bUOlrApmHcDsY+fBdg5mmlX
ddtyH5T0cHovbZrr/JI4jduIE9DwW3S9jV/Lz0fLM0DVIpHgjHeSckHp1bni0ERu
/ero4zM+0q3HZeZIgmvgDfNhmGcYA9hpi9sLib6tHF4MxkB0gr+eX1qIzaIxgI8K
/8mXEuyQhPMNzH8gbEbubOX6lkIxYRfLLklV0s93oPj6HyC656zesYOLiSKWWBJl
15Wi9w1frSKlbAtrUTrA1KxJINncdb7jRWuwyUrYaSykdwN22YpO/tSs0h5yB/6w
p/pSI0dYn3GdUaM3QjLGkXwxXvI0Jl+Ib0r07Ouk/avtoy6eM8v9SSW3ohVBjQID
AQABMA0GCSqGSIb3DQEBCwUAA4ICAQBpP4Gr2Fw6CIgosP3ZHoDs3N/OtKGUKF9O
LO2/MPPP3O54x1svFeZIGRFtaxrGqFkCs+SM+Ti9gw6KTq7FmLddqiKYy1bcIj2J
Py06m95tAVNtDhMYDtRVjNeEcdJEewprl55KoxU+XXHdJQ+VaTiIpNc8HBSDShb2
AMv9O0zvBCCpiLM/t5QE1d/f+NCZ7MNHRRs/JVq+YhqK6S4nMIbzC8uNJI1rlMxx
xNiSmvW+RybgU3JKBcKb6TujCR79Jt5kT1ZYmGLQd1VhS59/W0O8PGH8j1YXFKhw
yyYVuR6nBfjDMKwiRJNU5GdqKJ1/lKucYJzrP3xWxemnhdERu2DEgYCYydNSCfNo
CdBIh4Y5Mufswj0cYPoylfWy25NuLbqwLmhj9kEq5BMBNLiJwGnIpIdVBd59payr
M7vgLchoksFqutzVhWleAEXg1dJo+GUKT9aep/OWzRSFYqruAILKHgylkftFb2GA
tM4WxCuAsphZoewqBKvTvCdn8fmXFuWEOaZYfT8IvJ4R+7CfUwI6dA5xHRVxO6Yp
DszbHrMGz4tq39kUG1ylOtspMuFhEHo7Qz+bRJFeLYvvV8W414m+zSndBut+thkY
RBKeYvjZEvkpjCK2SQUK3SqipzpJFu5gkr3NcTk6Qd2T3LOAZcGXYLCTxTEW9DIn
8X3nbqwPPg==
-----END CERTIFICATE-----
`
)
