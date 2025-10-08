.EXPORT_ALL_VARIABLES:

SMTP_HOST = test.email.host
SMTP_PORT = 443
SMTP_USER = alpha@test.com
SMTP_PASS = very-secret-pass
SITES = my-site,product-alpha
MY_SITE_TO = hello@mysite.com
PRODUCT_ALPHA_TO = hello@produt.com

run: 
	go run cmd/api/main.go

test: 
	go test ./...	
