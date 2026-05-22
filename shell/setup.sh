# PASTE LINE BY LINE MANUALLY IN AN SSH CONNECTION

export service_name="membox-serv"
export dist_folder="membox-serv"
export SCRIPT_DIR="$HOME/$dist_folder/shell"
export serviceOrig="$SCRIPT_DIR/servicefile.service"
export serviceDest="/etc/systemd/system/$service_name.service"

sudo cp $serviceOrig $serviceDest

sudo systemctl daemon-reload
sudo systemctl restart $service_name
sudo systemctl enable $service_name
sudo systemctl status $service_name

# journalctl -u membox-serv -n 100 -f