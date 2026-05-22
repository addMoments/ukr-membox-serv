# line by line
./conn.sh
sudo systemctl daemon-reload
sudo systemctl restart localproxy
sudo systemctl status localproxy