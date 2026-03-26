server_host := "crosshatch"

default:
    just -l

deploy: _not_on_server
    GOOS=linux GOARCH=amd64 go build
    ssh {{server_host}} "sudo install -d -m 755 -o user -g user /opt/pottery-log-server"
    ssh {{server_host}} "mv /opt/pottery-log-server/pottery-log-server /opt/pottery-log-server/pottery-log-server.bak || echo no file"
    rsync -aiz .env pottery-log-server justfile *.service cleanup*.sh {{server_host}}:/opt/pottery-log-server/
    ssh {{server_host}} "cd /opt/pottery-log-server && just systemd-install"

systemd-install: _on_server
    sudo cp pottery-log-server.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl restart pottery-log-server
    tail /opt/pottery-log-server/pottery-log-server.log

test-remote:
    xh POST https://jesskenney.com/pottery-log/export --form deviceId=test1234 metadata=pretend --resolve jesskenney.com:15.204.93.96
    xh POST https://jesskenney.com/pottery-log/finish-export --form deviceId=test1234 --resolve jesskenney.com:15.204.93.96


_on_server:
    @{{ if `hostname` != server_host { error("must run on server") } else { "" } }}

_not_on_server:
    @{{ if `hostname` == server_host { error("must run locally") } else { "" } }}
