package fhttp

import (
	"fmt"
	"time"

	"github.com/bogdanfinn/fhttp/http2"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	tls "github.com/bogdanfinn/utls"
)

type Client interface {
	tlsclient.HttpClient
}

type client struct {
	tlsclient.HttpClient
}

func NewClient(timeout time.Duration, useJar bool, proxy string) (Client, error) {
	jar := tlsclient.NewCookieJar()
	secs := int(timeout.Seconds())
	if secs <= 0 {
		secs = 30
	}

	// Profile based on profiles.Chrome_133
	profile := profiles.NewClientProfile(
		tls.ClientHelloID{
			Client:               "Chrome",
			RandomExtensionOrder: false,
			Version:              "133",
			Seed:                 nil,
			SpecFactory: func() (tls.ClientHelloSpec, error) {
				return tls.ClientHelloSpec{
					CipherSuites: []uint16{
						tls.GREASE_PLACEHOLDER,
						tls.TLS_AES_128_GCM_SHA256,
						tls.TLS_AES_256_GCM_SHA384,
						tls.TLS_CHACHA20_POLY1305_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
						tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
						tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_RSA_WITH_AES_256_CBC_SHA,
					},
					CompressionMethods: []byte{
						tls.CompressionNone,
					},
					Extensions: []tls.TLSExtension{
						&tls.UtlsGREASEExtension{},
						&tls.SessionTicketExtension{},
						&tls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []tls.SignatureScheme{
							tls.ECDSAWithP256AndSHA256,
							tls.PSSWithSHA256,
							tls.PKCS1WithSHA256,
							tls.ECDSAWithP384AndSHA384,
							tls.PSSWithSHA384,
							tls.PKCS1WithSHA384,
							tls.PSSWithSHA512,
							tls.PKCS1WithSHA512,
						}},
						&tls.ApplicationSettingsExtension{
							CodePoint:          tls.ExtensionALPS,
							SupportedProtocols: []string{"h2"},
						},
						&tls.KeyShareExtension{KeyShares: []tls.KeyShare{
							{Group: tls.CurveID(tls.GREASE_PLACEHOLDER), Data: []byte{0}},
							{Group: tls.X25519MLKEM768},
							{Group: tls.X25519},
						}},
						&tls.SCTExtension{},
						&tls.SupportedPointsExtension{SupportedPoints: []byte{
							tls.PointFormatUncompressed,
						}},
						&tls.SupportedVersionsExtension{Versions: []uint16{
							tls.GREASE_PLACEHOLDER,
							tls.VersionTLS13,
							tls.VersionTLS12,
						}},
						&tls.StatusRequestExtension{},
						&tls.ALPNExtension{AlpnProtocols: []string{
							"h2",
							"http/1.1",
						}},
						&tls.SNIExtension{},
						tls.BoringGREASEECH(),
						&tls.UtlsCompressCertExtension{Algorithms: []tls.CertCompressionAlgo{
							tls.CertCompressionBrotli,
						}},
						&tls.SupportedCurvesExtension{Curves: []tls.CurveID{
							// Disabled due to incompatibility with some servers
							// tls.GREASE_PLACEHOLDER,
							// tls.X25519MLKEM768,
							tls.X25519,
							tls.CurveP256,
							tls.CurveP384,
						}},
						&tls.PSKKeyExchangeModesExtension{Modes: []uint8{
							tls.PskModeDHE,
						}},
						&tls.ExtendedMasterSecretExtension{},
						&tls.RenegotiationInfoExtension{
							Renegotiation: tls.RenegotiateOnceAsClient,
						},
						&tls.UtlsGREASEExtension{},
					},
				}, nil
			},
		},
		map[http2.SettingID]uint32{
			http2.SettingHeaderTableSize:   65536,
			http2.SettingEnablePush:        0,
			http2.SettingInitialWindowSize: 6291456,
			http2.SettingMaxHeaderListSize: 262144,
		},
		[]http2.SettingID{
			http2.SettingHeaderTableSize,
			http2.SettingEnablePush,
			http2.SettingInitialWindowSize,
			http2.SettingMaxHeaderListSize,
		},
		[]string{
			":method",
			":authority",
			":scheme",
			":path",
		},
		15663105,
		nil, nil,
	)

	options := []tlsclient.HttpClientOption{
		tlsclient.WithTimeoutSeconds(secs),
		tlsclient.WithClientProfile(profile),
	}
	if useJar {
		options = append(options, tlsclient.WithCookieJar(jar))
	}
	if proxy != "" {
		options = append(options, tlsclient.WithProxyUrl(proxy))
	}
	c, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("fhttp: couldn't create http client: %w", err)
	}
	return &client{HttpClient: c}, nil
}
