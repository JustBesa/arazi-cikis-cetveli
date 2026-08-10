# Arazi Çıkış Cetveli

Tarım ve Orman Bakanlığına bağlı veteriner hekimlerin aylık arazi çıkış kayıtlarını daha düzenli tutabilmesi için geliştirilmiş ücretsiz Windows masaüstü uygulaması.

Uygulama **JustBesa** tarafından geliştirilmiştir.

**Geliştirici / GitHub:** https://github.com/JustBesa

## Özellikler

- Yıl ve ay bazlı arazi çıkış kayıtları
- Tarihlerin otomatik oluşturulması
- Boş hafta sonlarının otomatik koyu renkle gösterilmesi
- Hafta sonuna veri girildiğinde satırın normal görünüme dönmesi
- Araç plakası, görev yeri, görev konusu ve açıklama alanları
- Veteriner hekim ve müdür bilgilerinin düzenlenmesi
- SQLite ile yerel veri saklama
- Otomatik ve manuel `.db` yedekleme
- A4 yatay tek sayfa PDF / yazdırma çıktısı
- İnternet bağlantısı olmadan çalışma
- İlk açılışta geliştirici teşekkür bildirimi

## İlk Açılış Bildirimi

Uygulama ilk çalıştırıldığında sağ alt köşede kısa bir teşekkür bildirimi gösterilir. Bildirim gösterildiği anda SQLite veritabanındaki `metadata` tablosuna kaydedilir ve sonraki açılışlarda tekrar gösterilmez.

Uygulamanın sağ alt köşesinde geliştirici bağlantısı kalıcı olarak bulunur:

**JustBesa · GitHub** — https://github.com/JustBesa

Bu geliştirici alanı PDF/yazdırma çıktısında görünmez.

## Veri Saklama

Veriler uzak bir sunucuya gönderilmez. Yerel SQLite veritabanında saklanır.

```text
%APPDATA%\AraziCikisCetveli\arazi-cikis-verileri.db
```

Yedekler aynı veri klasörü altındaki `Yedekler` klasöründe tutulur.

## İndirme

Hazır Windows sürümü için GitHub repository'sindeki **Releases** bölümünü kullanın.

Repository: https://github.com/JustBesa/arazi-cikis-cetveli

## Kaynak Koddan Derleme

```powershell
go test ./...
go build -trimpath -ldflags="-H=windowsgui -s -w" -o "Arazi-Cikis-Cetveli.exe" .
```

Özel uygulama ikonunu kullanıyorsanız `rsrc_windows_amd64.syso` dosyasının proje klasöründe bulunduğundan emin olun.

## Kullanılan Teknolojiler

- Go
- HTML
- CSS
- JavaScript
- SQLite
- Windows

## Sürüm

### v1.1.0

- Geliştirici adı ve GitHub bağlantısı uygulamaya eklendi.
- İlk açılışa özel teşekkür bildirimi eklendi.
- Bildirimin yalnızca bir kez gösterilmesi SQLite `metadata` tablosu ile kalıcı hale getirildi.
- Uygulama açıklaması Tarım ve Orman Bakanlığına bağlı veteriner hekimlere yönelik olacak şekilde güncellendi.

### v1.0.0

İlk kararlı sürüm.

## Geliştirici

**JustBesa**  
GitHub: https://github.com/JustBesa
