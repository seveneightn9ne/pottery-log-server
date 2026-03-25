set -e

GOOS=linux GOARCH=amd64 go build
ssh jessk@stipple "mv pottery-log-server/pottery-log-server pottery-log-server/pottery-log-server.bak"
scp pottery-log-server jessk@stipple:pottery-log-server/pottery-log-server
ssh jessk@stipple "sudo systemctl restart pottery-log-server"
ssh jessk@stipple "tail pottery-log-server/pottery-log-server.log"
