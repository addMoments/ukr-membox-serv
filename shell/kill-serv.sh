sudo systemctl stop membox-serv
sudo systemctl status membox-serv
journalctl -u membox-serv -n 100 -f