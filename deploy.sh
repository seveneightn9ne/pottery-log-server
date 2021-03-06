set -e

GOOS=linux GOARCH=amd64 go build
ssh stipple "mv pottery-log-server/pottery-log-server pottery-log-server/pottery-log-server.bak"
scp pottery-log-server stipple:pottery-log-server/pottery-log-server
ssh stipple "sudo systemctl restart pottery-log-server"
ssh stipple "tail pottery-log-server/pottery-log-server.log"