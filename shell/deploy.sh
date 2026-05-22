SCRIPT_DIR=$(dirname "$(readlink -f "$0")")

user_name=ubuntu
ec2_ip="16.171.47.166"
dist_folder="membox-serv"
service_name="membox-serv"

pem_path=$SCRIPT_DIR/membox-servs-key.pem

cd $SCRIPT_DIR
cd ..

PROJ_DIR=$(pwd)

folder_name=$(basename "$PROJ_DIR")
zip_path=$HOME/"$folder_name".tar.gz
zip_server_path="/home/"$user_name"/$dist_folder/$folder_name".tar.gz

include_folders=("src" "shell")
exclude_files=("shell/membox-servs-key.pem")
include_rootfiles=("main.go" "go.mod" "go.sum" ".env")

# Build exclude arguments as array
exclude_args=()
for file in "${exclude_files[@]}"; do
    exclude_args+=("--exclude=$file")
done

echo "compressing..."

tar -czf "$zip_path" "${exclude_args[@]}" -C "$PROJ_DIR" "${include_folders[@]}" "${include_rootfiles[@]}"

echo "copying..."

scp -i "$pem_path" $zip_path "$user_name"@"$ec2_ip":"$zip_server_path"

rm $zip_path

echo "remote execution..."

ssh -t -i "$pem_path" "$user_name"@"$ec2_ip" "\
cd $dist_folder &&\
tar -xvzf ./$folder_name.tar.gz &&\
rm ./$folder_name.tar.gz &&\
/usr/local/go/bin/go build main.go &&\
rm -r ./src &&\
sudo systemctl daemon-reload &&\
sudo systemctl restart $service_name &&\
sudo systemctl status $service_name &&\
exit"