SCRIPT_DIR=$(dirname "$(readlink -f "$0")")

user_name=ubuntu
ec2_ip="13.53.198.197"
dist_folder="db-proxy"
pem_path=$SCRIPT_DIR/membox-servs-key.pem

sudo rm $SCRIPT_DIR/nohup.out
echo "removing serv pid"
echo ----------------
cat $SCRIPT_DIR/serv_pid
echo ----------------
sudo rm $SCRIPT_DIR/serv_pid



cd $SCRIPT_DIR
# changed to local-proxy folder
cd local-proxy

PROJ_DIR=$(pwd)

folder_name=$(basename "$PROJ_DIR")
zip_path=$HOME/"$folder_name".tar.gz
zip_server_path="/home/"$user_name"/$dist_folder/$folder_name".tar.gz

echo "compressing..."

tar -czf $zip_path --exclude='.git' -C "$PROJ_DIR" .

echo "copying..."

scp -i "$pem_path" $zip_path "$user_name"@"$ec2_ip":"$zip_server_path"

rm $zip_path

echo "remote execution..."

ssh -i "$pem_path" "$user_name"@"$ec2_ip" "\
source ~/.zshrc &&\
cd $dist_folder&&\
tar -xvzf ./$folder_name.tar.gz&&\
rm ./$folder_name.tar.gz &&\
ls -la &&\
go build local-proxy.go&&\
rm local-proxy.go&&\
rm -rf ./src &&\
exit"