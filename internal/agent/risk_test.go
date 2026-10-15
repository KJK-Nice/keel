package agent

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		cmd  string
		want Risk
	}{
		// Read-only
		{"ls -la /var/log", RiskReadOnly},
		{"df -h", RiskReadOnly},
		{"tail -n 200 /var/log/syslog | grep -i error", RiskReadOnly},
		{"echo keel-$((6*7))", RiskReadOnly},
		{"echo $(date) $(hostname)", RiskReadOnly},
		{"ps aux | sort -k3 -nr | head", RiskReadOnly},
		{"journalctl -u nginx --since '1 hour ago'", RiskReadOnly},
		{"systemctl status postgresql", RiskReadOnly},
		{"sudo -u postgres psql -xc \"select client_addr, state from pg_stat_replication;\"", RiskReadOnly},
		{"kubectl get pods -A", RiskReadOnly},
		{"kubectl logs deploy/api --tail=100", RiskReadOnly},
		{"docker ps -a", RiskReadOnly},
		{"docker compose logs -f web", RiskReadOnly},
		{"git status && git log --oneline -5", RiskReadOnly},
		{"find /var/log -name '*.gz' -mtime +7", RiskReadOnly},
		{"curl -sS https://example.com/health", RiskReadOnly},
		{"cat /etc/os-release 2>/dev/null", RiskReadOnly},
		{"ls > /dev/null 2>&1", RiskReadOnly},
		{"grep -c foo file.txt # count", RiskReadOnly},
		{"FOO=1 env | grep FOO", RiskReadOnly},
		{"sed -n '1,20p' /etc/hosts", RiskReadOnly},
		{"timeout 5 ping -c 3 10.0.0.1", RiskReadOnly},
		{"crontab -l", RiskReadOnly},
		{"redis-cli -h db info replication", RiskReadOnly},
		{"ssh bastion uptime", RiskReadOnly},
		{"watch -n 2 df -h", RiskReadOnly},
		{"", RiskReadOnly},

		// Changes state
		{"touch /tmp/x", RiskChanges},
		{"echo hi > /tmp/x", RiskChanges},
		{"echo hi >> ~/.bashrc", RiskChanges},
		{"sudo systemctl restart nginx", RiskChanges},
		{"apt-get install -y htop", RiskChanges},
		{"sed -i 's/a/b/' /etc/app.conf", RiskChanges},
		{"curl -X POST https://api/x", RiskChanges},
		{"curl -d a=b https://api/x", RiskChanges},
		{"kill 1234", RiskChanges},
		{"mv a b", RiskChanges},
		{"kubectl apply -f deploy.yaml", RiskChanges},
		{"git pull", RiskChanges},
		{"psql", RiskChanges},
		{"psql -c 'update users set admin = true'", RiskChanges},
		{"some-unknown-tool --flag", RiskChanges},
		{"ls; touch x", RiskChanges},
		{"echo $(touch /tmp/y)", RiskChanges},
		{"bash -c 'chmod 600 key'", RiskChanges},
		{"echo 'unterminated", RiskChanges},
		{"ls | tee out.txt", RiskChanges},
		{"find . -name '*.tmp' -exec chmod 600 {} ;", RiskChanges},

		// Destructive
		{"rm -rf /tmp/keel-e2e", RiskDestructive},
		{"sudo rm -rf /var/lib/postgresql", RiskDestructive},
		{"/bin/rm file", RiskDestructive},
		{"\\rm file", RiskDestructive},
		{"find /tmp -name '*.log' -delete", RiskDestructive},
		{"find /tmp -name x -exec rm {} +", RiskDestructive},
		{"ls | xargs rm", RiskDestructive},
		{"ls | xargs -I{} rm {}", RiskDestructive},
		{"dd if=/dev/zero of=/dev/sda bs=1M", RiskDestructive},
		{"cat image.iso > /dev/sdb", RiskDestructive},
		{"mkfs.ext4 /dev/sdb1", RiskDestructive},
		{"git reset --hard HEAD~1", RiskDestructive},
		{"git push --force origin main", RiskDestructive},
		{"git clean -fdx", RiskDestructive},
		{"docker system prune -af", RiskDestructive},
		{"docker rm -f web", RiskDestructive},
		{"docker compose down -v", RiskDestructive},
		{"kubectl delete ns staging", RiskDestructive},
		{"helm uninstall api", RiskDestructive},
		{"psql -c 'DROP TABLE users'", RiskDestructive},
		{"mysql -e \"delete from sessions\"", RiskDestructive},
		{"redis-cli FLUSHALL", RiskDestructive},
		{"crontab -r", RiskDestructive},
		{"sudo reboot", RiskDestructive},
		{"terraform destroy -auto-approve", RiskDestructive},
		{"iptables -F", RiskDestructive},
		{"rsync -a --delete src/ dst/", RiskDestructive},
		{"ssh prod 'rm -rf /srv/app'", RiskDestructive},
		{"echo ok && truncate -s 0 /var/log/app.log", RiskDestructive},
		{"echo $(rm -rf ~)", RiskDestructive},
		{"sh -c \"rm -rf /tmp/x\"", RiskDestructive},
	}
	for _, c := range cases {
		if got := Classify(c.cmd); got != c.want {
			t.Errorf("Classify(%q) = %s, want %s", c.cmd, got, c.want)
		}
	}
}

func TestNeedsShift(t *testing.T) {
	cases := []struct {
		risk       Risk
		production bool
		want       bool
	}{
		{RiskReadOnly, false, false},
		{RiskReadOnly, true, false},
		{RiskChanges, false, false},
		{RiskChanges, true, true},
		{RiskDestructive, false, true},
		{RiskDestructive, true, true},
	}
	for _, c := range cases {
		if got := NeedsShift(c.risk, c.production); got != c.want {
			t.Errorf("NeedsShift(%s, production=%v) = %v, want %v", c.risk, c.production, got, c.want)
		}
	}
}

func TestStricterNeverLoosens(t *testing.T) {
	all := []Risk{RiskReadOnly, RiskChanges, RiskDestructive}
	for _, a := range all {
		for _, b := range all {
			got := Stricter(a, b)
			if got.rank() < a.rank() || got.rank() < b.rank() {
				t.Errorf("Stricter(%s, %s) = %s", a, b, got)
			}
		}
	}
	if Stricter("", RiskReadOnly) != RiskChanges {
		t.Error("an unrated risk must count as changes")
	}
}
