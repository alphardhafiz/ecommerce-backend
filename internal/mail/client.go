package mail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const resendAPIURL = "https://api.resend.com/emails"

type Client struct {
	apiKey    string
	from      string
	http      *http.Client
	resendURL string
}

func New(apiKey, from string) *Client {
	return &Client{
		apiKey:    apiKey,
		from:      from,
		http:      &http.Client{Timeout: 10 * time.Second},
		resendURL: resendAPIURL,
	}
}

// SendPasswordReset sends the reset-password email. If no API key is set (dev
// without Resend), the link is logged instead so the flow still works locally.
func (c *Client) SendPasswordReset(to, resetLink string) error {
	if c.apiKey == "" {
		slog.Info("reset email (dev mode, RESEND_API_KEY not set)", "to", to, "reset_link", resetLink)
		return nil
	}

	payload := map[string]any{
		"from":    c.from,
		"to":      []string{to},
		"subject": "Atur ulang password — Ledger & Tag",
		"html": `
<!DOCTYPE html>
<html lang="id">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Atur Ulang Password</title>
</head>

<!--
	Brand: Ledger & Tag (alat tulis kertas & kayu).
	Token: paper #EDE8DC, paper-raised #F7F4EC, ink #161D1A, stamp #A63D2F,
	taupe #B8AF9C, taupe-dark #6E6656, mustard #C98A2C.
	Anti-pattern DESIGN §9: tanpa gradient, tanpa shadow tebal, radius kecil,
	tanpa emoji di copy. Serif (Georgia) untuk display, sans (Arial) untuk body,
	mono (Courier New) untuk label kecil.
-->

<body style="
	margin: 0;
	padding: 0;
	background-color: #EDE8DC;
	font-family: Arial, Helvetica, sans-serif;
	color: #161D1A;
">

<table
	width="100%"
	cellpadding="0"
	cellspacing="0"
	border="0"
	style="background-color: #EDE8DC; padding: 40px 16px;"
>
	<tr>
		<td align="center">

			<!-- Container -->
			<table
				width="100%"
				cellpadding="0"
				cellspacing="0"
				border="0"
				style="
					max-width: 560px;
					background-color: #F7F4EC;
					border: 1px solid #B8AF9C;
					border-radius: 4px;
				"
			>

				<!-- Header -->
				<tr>
					<td
						align="center"
						style="
							padding: 32px 24px 24px;
							background-color: #161D1A;
						"
					>
						<div style="
							font-family: Georgia, 'Times New Roman', serif;
							font-size: 26px;
							color: #EDE8DC;
						">
							Ledger <span style="font-style: italic;">&amp;</span> Tag
						</div>

						<div style="
							margin-top: 8px;
							font-family: 'Courier New', Courier, monospace;
							font-size: 11px;
							letter-spacing: 0.06em;
							color: #B8AF9C;
						">
							ALAT TULIS KERTAS &amp; KAYU
						</div>
					</td>
				</tr>

				<!-- Content -->
				<tr>
					<td style="padding: 32px 40px 8px;">

						<h1 style="
							margin: 0 0 16px;
							font-family: Georgia, 'Times New Roman', serif;
							font-size: 26px;
							font-weight: normal;
							line-height: 1.3;
							color: #161D1A;
						">
							Atur Ulang Password
						</h1>

						<p style="
							margin: 0 0 16px;
							font-size: 15px;
							line-height: 1.7;
							color: #6E6656;
						">
							Kami menerima permintaan untuk mengatur ulang password
							akun Anda. Klik tombol di bawah untuk membuat password
							baru.
						</p>

					</td>
				</tr>

				<!-- Button -->
				<tr>
					<td align="center" style="padding: 16px 40px 8px;">
						<a
							href="` + resetLink + `"
							style="
								display: inline-block;
								padding: 12px 28px;
								background-color: #A63D2F;
								color: #EDE8DC;
								text-decoration: none;
								font-size: 15px;
								font-weight: 600;
								border-radius: 4px;
							"
						>
							Buat Password Baru
						</a>
					</td>
				</tr>

				<!-- Expiration Notice -->
				<tr>
					<td style="padding: 24px 40px 8px;">
						<table
							width="100%"
							cellpadding="0"
							cellspacing="0"
							border="0"
							style="
								border: 1px solid #C98A2C;
								border-radius: 4px;
							"
						>
							<tr>
								<td style="
									padding: 12px 16px;
									font-size: 13px;
									line-height: 1.6;
									color: #6E6656;
								">
									<strong style="color: #161D1A;">Link berlaku selama 30 menit.</strong>
									Setelah itu, minta link baru melalui halaman lupa
									password.
								</td>
							</tr>
						</table>
					</td>
				</tr>

				<!-- Fallback Link -->
				<tr>
					<td style="padding: 24px 40px 8px;">
						<p style="
							margin: 0 0 8px;
							font-size: 13px;
							line-height: 1.6;
							color: #6E6656;
						">
							Jika tombol di atas tidak dapat digunakan, salin dan buka
							link berikut di browser Anda:
						</p>

						<p style="
							margin: 0;
							font-size: 12px;
							line-height: 1.6;
							word-break: break-all;
						">
							<a
								href="` + resetLink + `"
								style="
									color: #A63D2F;
									text-decoration: none;
								"
							>
								` + resetLink + `
							</a>
						</p>
					</td>
				</tr>

				<!-- Security Notice (dipisah garis perforasi) -->
				<tr>
					<td style="padding: 32px 40px 32px;">
						<table
							width="100%"
							cellpadding="0"
							cellspacing="0"
							border="0"
							style="
								border-top: 1px dashed #B8AF9C;
							"
						>
							<tr>
								<td style="
									padding-top: 24px;
									font-size: 13px;
									line-height: 1.6;
									color: #6E6656;
								">
									<strong style="color: #161D1A;">
										Bukan Anda yang meminta reset password?
									</strong>
									<br>
									Abaikan email ini. Password Anda tidak akan berubah
									sampai Anda menggunakan link reset tersebut.
								</td>
							</tr>
						</table>
					</td>
				</tr>

				<!-- Footer -->
				<tr>
					<td
						align="center"
						style="
							padding: 24px;
							border-top: 1px solid #B8AF9C;
						"
					>
						<p style="
							margin: 0;
							font-size: 12px;
							color: #6E6656;
							line-height: 1.6;
						">
							Email ini dikirim secara otomatis. Mohon jangan membalas
							email ini.
						</p>

						<p style="
							margin: 8px 0 0;
							font-family: 'Courier New', Courier, monospace;
							font-size: 11px;
							letter-spacing: 0.04em;
							color: #6E6656;
						">
							© 2026 LEDGER &amp; TAG — ALAT TULIS KERTAS &amp; KAYU
						</p>
					</td>
				</tr>

			</table>

		</td>
	</tr>
</table>

</body>
</html>
`,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, c.resendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend API status %d", resp.StatusCode)
	}
	return nil
}
