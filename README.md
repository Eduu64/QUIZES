# QuizBox

App web minimalista para estudiar tests tipo test (oposiciones, exámenes, repaso…) organizados en carpetas. Un único binario en Go de unos 5 MB sin ninguna dependencia externa: el servidor embebe la interfaz, la comprime una sola vez al arrancar y la sirve desde memoria.

Todo tu contenido son archivos normales dentro de una carpeta: subcarpetas y ficheros `.json` legibles con cualquier editor. Hacer una copia de seguridad es copiar esa carpeta.

## Características

- 📁 **Organización en árbol**: carpetas y subcarpetas ilimitadas, con contador de tests por carpeta.
- 📝 **Tests `.json`**: preguntas de una respuesta o de varias correctas, con explicación opcional por pregunta.
- ➕ **Añadir tests** subiendo varios archivos a la vez o pegando un JSON; detecta duplicados y los numera `(2)`, `(3)`…
- ✋ **Arrastrar y soltar**: suelta `.json` desde tu equipo sobre cualquier carpeta, o reordena tests y carpetas dentro de la app.
- ✏️ **Menú «⋯»** en cada carpeta y test: renombrar, mover o borrar.
- ✅ **Modo estudio**: corrección inmediata opcional, corregir todo al final, repetir solo las falladas, ver respuesta y explicación de cada pregunta.
- 💾 **Progreso guardado en el navegador** (localStorage): cierras y continúas donde lo dejaste, sin cuentas ni bases de datos.
- 🌓 **Modo claro y oscuro** automáticos según tu sistema.
- 📱 **Responsive**: pensada para usar desde el móvil a la vez que en el escritorio.
- ⚡ **Muy ligera**: ~10 MB de RAM en uso, interfaz de menos de 9 KB comprimida, ETag para no reenviar nada que no cambie.
- 🏠 **Pensada para tu red de confianza**: sin cuentas, sin contraseñas, sin configuración. Las rutas están blindadas internamente contra `path traversal`.

## Despliegue con Docker

```bash
docker compose up -d --build
```

La app queda servida en `http://localhost:8080`. Para exponerla solo en tu LAN, cambia el mapeo de puertos en `docker-compose.yml` (p. ej. `"192.168.1.50:8080:8080"`).

El compose no incluye ningún endurecimiento del contenedor (no hay `cap_drop`, ni rootfs de solo lectura, ni contraseñas): así el contenedor puede escribir en el volumen sin problemas de permisos aunque la carpeta del host pertenezca a tu usuario.

### Persistencia de los tests

Los tests viven en la carpeta `./data` **de tu servidor**, montada dentro del contenedor en `/data`. Eso significa que son **100% persistentes**: puedes reconstruir la imagen (`docker compose up -d --build`), actualizar la app, parar o borrar el contenedor, y tus carpetas y tests seguirán ahí, porque no se guardan dentro del contenedor sino en disco.

Copia de seguridad: copia la carpeta `./data` y ya está.

Si prefieres un volumen gestionado por Docker (p. ej. para no depender de la ruta del proyecto), comenta la línea `./data:/data` y descomenta `quizbox-data:/data` en `docker-compose.yml`; para llevar tus tests actuales al volumen:

```bash
docker compose up -d
docker cp ./data/. quizbox:/data/
```

> Si algún día ves un error `permission denied` al crear o borrar, revisa quién es el dueño de `./data` en el host (`ls -la data`). Cualquier usuario normal de Docker (root del contenedor, o tu uid con Docker rootless) puede escribir en ella; basta con `sudo chown -R $(id -u):$(id -g) data` si viene de otra instalación.

### Sin Docker

Requiere Go 1.22 o superior:

```bash
go build -o quizbox
DATA_DIR=./data ADDR=:8080 ./quizbox
```

## Configuración

| Variable | Por defecto | Descripción |
|---|---|---|
| `DATA_DIR` | `/data` | Carpeta donde viven las carpetas y los tests |
| `ADDR` | `:8080` | Dirección y puerto de escucha |

En el `Dockerfile` se ajustan además `GOGC`, `GOMEMLIMIT` y `GOMAXPROCS` para contener el consumo de memoria. No hay más configuración: sin usuarios ni contraseñas.

## Formato de un test

```json
{
  "title": "ITA Informática 2025 - Acceso Libre",
  "questions": [
    {
      "q": "¿Capital de España?",
      "options": ["Madrid", "Barcelona", "Sevilla"],
      "answer": 0,
      "explanation": "Opcional: se muestra al corregir o pulsar el ojo"
    },
    {
      "q": "¿Cuáles son números pares?",
      "options": ["2", "3", "4"],
      "answer": [0, 2]
    }
  ]
}
```

- `answer` admite un número (una sola correcta) o una lista (varias correctas). Los índices empiezan en **0**.
- `explanation` es opcional.
- Máximo 4 MB por archivo. El servidor valida el JSON al subirlo (título, mínimo 2 opciones, respuestas dentro de rango).

## Uso diario

1. **Organiza**: botón `+ Carpeta` (por temario, año, bloque…). Los tests pueden vivir en el inicio o dentro de carpetas anidadas.
2. **Añade tests**: botón `Añadir tests` o arrastrando archivos `.json` al árbol.
3. **Estudia**: abre un test, responde y usa `Corregir` al final (o activa «Corrección inmediata»). Con `Repetir falladas` te quedas solo con las preguntas que fallaste o dejaste en blanco.
4. **Mantén**: con «⋯» renombras, mueves o borras; con arrastrar y soltar reordenas en un segundo.

## API

La app se sirve de una API JSON sencilla por si quieres automatizar (scripts, sincronización…):

| Método | Ruta | Acción |
|---|---|---|
| GET | `/api/tree` | Árbol completo de carpetas y tests |
| GET | `/api/quiz?path=…` | Descargar un test |
| PUT | `/api/quiz?path=…` | Crear o sobrescribir un test (JSON validado) |
| DELETE | `/api/quiz?path=…` | Borrar un test |
| POST | `/api/folder?path=…` | Crear una carpeta |
| DELETE | `/api/folder?path=…` | Borrar una carpeta y todo su contenido |
| POST | `/api/move?from=…&to=…` | Renombrar o mover |
