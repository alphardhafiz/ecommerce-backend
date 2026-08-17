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
	"subject": "Reset password",
	"html": `
<!DOCTYPE html>
<html lang="id">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Reset Password</title>
</head>

<body style="
	margin: 0;
	padding: 0;
	background-color: #f4f6f8;
	font-family: Arial, Helvetica, sans-serif;
	color: #1f2937;
">

<table
	width="100%"
	cellpadding="0"
	cellspacing="0"
	border="0"
	style="background-color: #f4f6f8; padding: 40px 16px;"
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
					background-color: #ffffff;
					border-radius: 16px;
					overflow: hidden;
					box-shadow: 0 4px 20px rgba(0,0,0,0.06);
				"
			>

				<!-- Header -->
				<tr>
					<td
						align="center"
						style="
							padding: 32px 24px;
							background-color: #111827;
						"
					>
						<div style="
							font-size: 24px;
							font-weight: 700;
							color: #ffffff;
							letter-spacing: -0.5px;
						">
							Your Store
						</div>

						<div style="
							margin-top: 8px;
							font-size: 14px;
							color: #9ca3af;
						">
							Keamanan akun Anda
						</div>
					</td>
				</tr>

				<!-- Content -->
				<tr>
					<td style="padding: 40px 40px 32px;">

						<div style="
							width: 56px;
							height: 56px;
							line-height: 56px;
							text-align: center;
							border-radius: 50%;
							background-color: #eef2ff;
							font-size: 26px;
							margin-bottom: 24px;
						">
							🔐
						</div>

						<h1 style="
							margin: 0 0 16px;
							font-size: 26px;
							line-height: 1.3;
							color: #111827;
						">
							Reset Password
						</h1>

						<p style="
							margin: 0 0 16px;
							font-size: 15px;
							line-height: 1.7;
							color: #4b5563;
						">
							Kami menerima permintaan untuk mengatur ulang password
							akun Anda.
						</p>

						<p style="
							margin: 0 0 28px;
							font-size: 15px;
							line-height: 1.7;
							color: #4b5563;
						">
							Klik tombol di bawah untuk membuat password baru.
						</p>

						<!-- Button -->
						<table
							cellpadding="0"
							cellspacing="0"
							border="0"
							width="100%"
						>
							<tr>
								<td align="center">
									<a
										href="` + resetLink + `"
										style="
											display: inline-block;
											padding: 14px 28px;
											background-color: #4f46e5;
											color: #ffffff;
											text-decoration: none;
											font-size: 15px;
											font-weight: 600;
											border-radius: 10px;
										"
									>
										Reset Password
									</a>
								</td>
							</tr>
						</table>

						<!-- Expiration Notice -->
						<table
							width="100%"
							cellpadding="0"
							cellspacing="0"
							border="0"
							style="
								margin-top: 28px;
								background-color: #fff7ed;
								border-radius: 10px;
							"
						>
							<tr>
								<td style="
									padding: 16px;
									font-size: 13px;
									line-height: 1.6;
									color: #9a3412;
								">
									<strong>⏱ Link berlaku selama 30 menit.</strong><br>
									Setelah itu, Anda perlu meminta link reset password
									baru.
								</td>
							</tr>
						</table>

						<!-- Fallback Link -->
						<p style="
							margin: 28px 0 8px;
							font-size: 13px;
							line-height: 1.6;
							color: #6b7280;
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
									color: #4f46e5;
									text-decoration: none;
								"
							>
								` + resetLink + `
							</a>
						</p>

					</td>
				</tr>

				<!-- Security Notice -->
				<tr>
					<td style="
						padding: 0 40px 32px;
					">
						<table
							width="100%"
							cellpadding="0"
							cellspacing="0"
							border="0"
							style="
								border-top: 1px solid #e5e7eb;
							"
						>
							<tr>
								<td style="
									padding-top: 24px;
									font-size: 13px;
									line-height: 1.6;
									color: #6b7280;
								">
									<strong style="color: #374151;">
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
							background-color: #f9fafb;
							border-top: 1px solid #f3f4f6;
						"
					>
						<p style="
							margin: 0;
							font-size: 12px;
							color: #9ca3af;
							line-height: 1.6;
						">
							Email ini dikirim secara otomatis. Mohon jangan membalas
							email ini.
						</p>

						<p style="
							margin: 8px 0 0;
							font-size: 12px;
							color: #9ca3af;
						">
							© 2026 Your Store. All rights reserved.
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
