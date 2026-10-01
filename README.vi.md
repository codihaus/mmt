<img src="docs/icon.png" width="96" align="right" alt="">

# mmt

Mattermost chạy trong terminal.

Mình ngồi trong terminal gần như cả ngày, mỗi lần có người tag lại phải qua
tab trình duyệt nên thấy phiền, thế là viết cái này. Sidebar giữ nguyên cách
bạn sắp xếp trên web, thread mở ở panel bên phải, còn nếu dùng iTerm2 thì ảnh
hiện luôn trong khung chat.

[English](README.md)

![mmt: sidebar, kênh đang mở và một tin nhắc tên](docs/screenshot.png)

App còn mới. Ngày nào mình cũng dùng với server thật, nhưng chắc chắn vẫn còn
chỗ chưa mượt, gặp lỗi thì cứ mở issue nhé.

## Cài đặt

Tải file nén hợp với máy ở [trang releases](https://github.com/codihaus/mmt/releases).
Bản macOS chạy được cả Mac chip Apple lẫn Intel.

```sh
tar -xzf mmt-macos-*.tar.gz && sh mmt-macos/install.sh
```

Script chép file `mmt` vào `~/.local/bin`, vậy là xong, không phải cài Go hay
thứ gì khác. Bản build chưa được Apple notarize nên cài qua terminal như trên;
double-click vào file sẽ bị Gatekeeper chặn.

Muốn tự build thì cần Go 1.26.7 trở lên:

```sh
go install github.com/codihaus/mmt@latest
```

Mình chủ yếu chạy trên macOS. Linux cũng chạy được, có `xdg-open`, `wl-copy`
hoặc `xclip`, `notify-send` thì app dùng. Dán ảnh từ clipboard hiện chỉ có
trên Mac.

Trên Windows, giải nén `mmt-windows-amd64-….zip` (hoặc `arm64`), chạy script
cài trong PowerShell rồi mở một cửa sổ Windows Terminal mới:

```powershell
powershell -ExecutionPolicy Bypass -File .\mmt-windows-amd64\install.ps1
```

Script chép `mmt.exe` vào `%LOCALAPPDATA%\Programs\mmt` và thêm thư mục đó vào
PATH. Bản Windows còn mới và gần như chưa được test, coi như bản xem trước:
chat, thread và lệnh chắc là ổn, thông báo đi qua toast của PowerShell, còn
dán ảnh, ảnh inline và agent chạy ngầm thì chưa có. Ai dùng Windows thì báo
giúp mình nhé. Nên dùng Windows Terminal, cửa sổ console cũ không vẽ đúng giao diện.

## Lần đầu chạy

```sh
mmt login
mmt
```

`mmt login` hỏi địa chỉ server và personal access token (tạo trong Mattermost ở
Profile → Security → Personal Access Tokens). Đăng nhập bằng username/password
cũng được, có MFA cũng không sao. Token được cất trong keychain của máy.

Chưa có server để thử? `make demo` bật một server giả trong `tools/mockserver`
rồi mở mmt vào đó.

## Dùng thế nào

Cơ bản thì như mọi app chat: gõ rồi Enter, `Shift+Enter` để xuống dòng.
`Ctrl+K` nhảy tới kênh hoặc người, gõ không dấu vẫn ra (`co hoi` tìm được
`Cơ hội`), tìm được cả kênh bạn chưa vào.

Dùng chuột được: click kênh để mở, click hai lần vào tin để mở thread, lăn
chuột để cuộn, kéo để bôi chọn chữ. `Ctrl+V` dán ảnh chụp màn hình, kéo file
vào cửa sổ để đính kèm.

| Phím | |
| --- | --- |
| `Enter` / `Shift+Enter` | Gửi / xuống dòng. Terminal nào không nhận `Shift+Enter` thì gõ `\` rồi `Enter` |
| `Ctrl+K` | Tìm kênh hoặc người |
| `Alt+↑` `Alt+↓` | Kênh trước / kế (hoặc `Ctrl+P` / `Ctrl+N`) |
| `Alt+A` | Kênh chưa đọc kế tiếp |
| `Ctrl+T` | Đổi team |
| `↑` khi ô nhập trống | Chọn tin. `Enter` mở thread, `o` mở tệp đính kèm, `y` copy link, `w` mở trên trình duyệt |
| `Tab` | Chuyển giữa sidebar, kênh và thread |
| `Esc` | Đóng cái đang mở |
| `Ctrl+L` | Khoá app (sau khi đã chạy `mmt lock`) |
| `F1` | Xem hết phím tắt |

### Lệnh

Gõ `/` để xem hết. Mấy lệnh quản trị mình thêm vào sẽ mở một form nhỏ, hỏi
từng thứ một và gợi ý người, kênh trong lúc gõ, vì chính mình cũng không nhớ
nổi cú pháp. Nếu gõ sẵn tham số (`/channel new Marketing --private`) thì form
bỏ qua những gì đã có. Lệnh nào xoá hay thu hồi thì đều hỏi lại trước.

- `/channel new | rename | info | archive`
- `/add @ai-do`, `/remove @ai-do`, `/team add @ai-do`
- `/dm @a @b`: nhắn riêng, nhiều tên thì thành nhóm chat
- `/user @ai-do`
- `/bot new | list | token | add` và `/token new | list | revoke`
- `/open`, `/link`: mở hoặc copy link web của kênh hay thread đang xem
- `/call`: vào cuộc gọi trong kênh hiện tại

Token mới chỉ hiện một lần và được copy thẳng vào clipboard, không bị đăng lên
đâu cả. Lệnh nào mmt không biết thì gửi lên server, nên `/away`, `/header`...
vẫn dùng như trên web.

### Chạy lệnh shell

Gõ `!` ở đầu để chạy lệnh trên máy thay vì gửi tin: `!git log -3`,
`!open ~/Downloads`. Kết quả hiện trong một popup và chỉ nằm trên máy bạn, trừ
khi bấm `Enter` thì nó được gửi lên chat dạng khối code. Lệnh không in ra gì
thì popup tự đóng. Lệnh không nhận được bàn phím nên `vim`, `ssh` không dùng ở
đây được, chạy quá 60 giây thì bị dừng. Muốn gửi tin bắt đầu bằng `!` thật thì
gõ `\!`.

### Cuộc gọi

Terminal không có âm thanh nên mmt không cố làm phần này. Cuộc gọi hiện thành
thẻ, có người gọi riêng thì có thông báo, click vào thẻ (hoặc gõ `/call`) để mở
kênh trong app Mattermost desktop rồi vào gọi từ đó.

### Thông báo khi đã tắt mmt

```sh
mmt background on
```

Lệnh này bật một agent nhỏ chạy ngầm (LaunchAgent, khởi động lại máy vẫn tự
chạy). Nó giữ kết nối và hiện thông báo macOS khi có người nhắc tên, nhắn
riêng hay gọi, kể cả lúc không mở cửa sổ mmt nào. Bấm vào thông báo là mmt mở
đúng kênh đó. Khi mmt đang mở thì agent im lặng, nên không bị báo hai lần.
Lần đầu macOS sẽ hỏi có cho mmt gửi thông báo không, chọn Cho phép.
`mmt background status` để xem agent có chạy không, `mmt background off` để
gỡ. `mmt setup` cũng hỏi phần này.

## Phím Cmd trên macOS

Terminal.app giữ hết phím `Cmd` cho menu của nó, nên app chạy bên trong không
bao giờ nhận được `Cmd+K`. iTerm2 và Ghostty thì chuyển được. Máy có một trong
hai thì `mmt` tự mở lại trong đó, map sẵn `Cmd+K`, `Cmd+V`, `Cmd+T`, `Cmd+↑/↓`
và `Shift+Enter`.

Với iTerm2, app thêm một profile riêng tên `mmt` (Dynamic Profile), profile
của bạn giữ nguyên. Với Ghostty thì phím được truyền qua tham số dòng lệnh.

`mmt --here` thì ở lại terminal hiện tại. Đang trong tmux, screen, zellij hay
SSH thì app không tự chuyển đi đâu.

## Khoá app

`mmt lock` đặt một lớp khoá riêng, không liên quan tới đăng nhập Mattermost:
Touch ID kèm mật khẩu dự phòng, hoặc chỉ mật khẩu. Từ đó mmt hỏi trước khi
hiện bất cứ thứ gì, `Ctrl+L` khoá ngay, và app tự khoá sau số phút không dùng
mà bạn chọn. Lúc khoá, thông báo chỉ ghi "Tin nhắn mới". Mật khẩu được lưu
dạng băm PBKDF2 trong keychain.

## Cấu hình

Nằm hết trong `~/.config/mmt/config.json`, mấy thứ hay đổi thì `mmt setup` làm
giúp.

| Khoá | |
| --- | --- |
| `server_url` | `mmt login` ghi vào |
| `language` | `en` hoặc `vi`. Để trống thì theo `LANG` |
| `terminal` | `auto`, `iterm2`, `ghostty` hoặc `current` |
| `browser` | App mở link, ví dụ `"Google Chrome"`. Để trống thì dùng trình duyệt mặc định |
| `lock`, `lock_after_minutes` | `mmt lock` ghi vào |

Vài biến môi trường ghi đè config: `MMT_URL` và `MMT_TOKEN` (tiện khi viết
script hay chạy demo), `MMT_LANG`, `MMT_NO_IMAGES=1` để tắt ảnh inline, và
`MMT_MULTIPLEXER=1` nếu bạn dùng multiplexer nào đó mà mmt không nhận ra.

## Bảo mật

Những gì người khác viết (tin nhắn, tên, header kênh, tên file) đều được lọc
bỏ ký tự điều khiển terminal trước khi hiển thị, nên một tin nhắn không thể đổi
tiêu đề cửa sổ, ghi vào clipboard hay gửi mã lệnh cho iTerm2. Tệp mở bằng phím
`o` được tải vào một thư mục tạm riêng và gắn cờ cho Gatekeeper. Ảnh, PDF, văn
bản và media thì mở thẳng, loại khác chỉ hiện trong Finder.

Phát hiện lỗi bảo mật thì báo qua
[private advisory](https://github.com/codihaus/mmt/security/advisories/new),
đừng mở issue công khai nhé.

## Phát triển

```sh
make test          # go test -race ./...
make lint          # vet, staticcheck, gofmt
make demo          # server giả + mmt
make video-server  # kịch bản dựng sẵn mình dùng để quay màn hình
make video         # mở mmt vào đó
```

Xem thêm [CONTRIBUTING.md](CONTRIBUTING.md).

## Giấy phép

Apache 2.0, xem [LICENSE](LICENSE).
