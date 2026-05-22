SCRIPT_DIR=$(dirname "$(readlink -f "$0")")
FILE_NAME="membox-servs-key.pem"
echo $SCRIPT_DIR/$FILE_NAME
ssh -i $SCRIPT_DIR/$FILE_NAME ubuntu@16.171.47.166
