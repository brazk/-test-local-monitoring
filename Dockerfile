FROM quay.io/prometheus/busybox:glibc

COPY /bin/linux_amd64/sql_exporter /bin/sql_exporter

ENTRYPOINT ["sql_exporter"]
