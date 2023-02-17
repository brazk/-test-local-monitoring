# Prometheus SQL Exporter 

[![Docker Pulls](https://img.shields.io/docker/pulls/justwatch/sql_exporter.svg?maxAge=604800)](https://hub.docker.com/r/justwatch/sql_exporter)


<h1 align="center">Run test-local-monitoring

<b>1. Git - клонирование репозитория на свой ПК.</b>

 Для этого потребуется IDE и Git, рекомендую использовать бесплатный редактор VS Code. Репозиторий здесь пользуемся веткой master.

 Команда для клонирования на примере этого репозитория:

 ```
 - git clone https://gitlab.ozon.ru/team-911/learning/test-local-monitoring.git
```

<b>Running in Docker:</b>

<b>2. Запуск docker-compose файла.</b>

 Перейти в папку с репозиторием и выполнить команду для запуска, после чего все контейнеры должны запуститься об этом будет информация в терминале.

```
docker-compose up -d
```
![Container status](Images/run.docker.png)

<b>3. Проверка работы контейнеров.</b>

 Выполнив команду увидим статус каждого из них. 

```
docker ps
```
![Checking_the_operation_of_containers](Images/checking_the_operation_of_containers.png)

<b>4. В случае проблем при запуске одного из контейнеров.</b>

Нужно проверить файл конфигурации в этом поможет команда:
```
docker logs names/container ID
```
![](Images/container_startup_issues.png)

<b>Ниже доп команды которые могут пригодиться:</b>

<i>Остановить все контейнеры.</i>
```
docker stop $(docker ps -q -a)
```

<i>Удалить все контейнеры.</i>
```
docker rm $(docker ps -q -a)
```

<i>Перезапуск контейнеров.</i>
```
docker restart $(docker ps -q -a)
```

<h1 align="center">Работа с postgreSQL

<b>5.Через расширение в VS Code - PostgreSQL Management Tool запускаем базу.</b>
Для подключения к базе нужно указать параметры (hostname, user, password, port, имя базы) они есть в файле docker-compose. Ниже скрин как должно оно выглядеть, здесь мы можем внести любой запрос нажав правой  кнопкой по названию базы и выбрав поле "New Query". БД нужно наполнить самостоятельно используя стандартную команду для создания таблиц SQL

![](Images/run_postgreSQL.png)




<b>Также можно подключиться через CLI:</b>

1. Авторизоваться, чтобы начать использовать как пользователь postgres. psql -U jarvis_user -h localhost -d jarvis_test
2. Ввести пароль, используемый при создании контейнера сервера PSQL.



<b>6. В итоге мы получаем полноценную тестовую среду для проверки/создания алертов без использования STG.</b>
Для подключения к любому из контейнеров используем адрес в браузере например - http://localhost:9091/ здесь всё зависит от маппинга портов. Ниже приведен пример отображения одной из метрик в prometheus   которая добавлена в sql-exporter.
![](Images/prometheus.png)



<b>Требуется установить следующее ПО:</b>

- IDE - рекомендация к установке Visual Studio Code
- GitLab
- Docker
- Дополнение в Visual Studio Code "PostgreSQL Management Tool"
