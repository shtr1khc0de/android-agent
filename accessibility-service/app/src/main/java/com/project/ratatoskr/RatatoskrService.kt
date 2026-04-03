package com.project.ratatoskr

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.AccessibilityServiceInfo
import android.accessibilityservice.GestureDescription
import android.graphics.Path
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import java.io.InputStream
import java.io.OutputStream
import java.net.Socket
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

class RatatoskrService : AccessibilityService() {
    private val nodeCounter = AtomicInteger(0)
    private val executor = Executors.newSingleThreadExecutor()
    private val mainHandler = Handler(Looper.getMainLooper())

    private var outputStream: OutputStream? = null
    private var inputStream: InputStream? = null
    private var socket: Socket? = null
    private var isConnecting = false
    private var isReadingCommands = false

    companion object {
        private const val TAG = "RATATOSKR"
        private const val NIDHOGG_HOST = "127.0.0.1"  // localhost для эмулятора/контейнера
        private const val NIDHOGG_PORT = 9999
    }



    private fun connectToAgent() {
        if (isConnecting) return
        isConnecting = true

        executor.execute {
            while (true) {
                try {
                    Log.d(TAG, "Trying to connect to Nidhogg...")

                    val newSocket = Socket(NIDHOGG_HOST, NIDHOGG_PORT)
                    outputStream = newSocket.getOutputStream()
                    inputStream = newSocket.getInputStream()
                    socket = newSocket

                    isConnecting = false
                    Log.d(TAG, "Connected to Nidhogg successfully")

                    // Запускаем чтение команд
                    startReadingCommands()
                    break

                } catch (e: Exception) {
                    Log.e(TAG, "Connection failed: ${e.message}. Retrying in 5s...")
                    try {
                        Thread.sleep(5000)
                    } catch (ie: InterruptedException) {
                        isConnecting = false
                        return@execute
                    }
                }
            }
        }
    }

    private fun startReadingCommands() {
        if (isReadingCommands) return
        isReadingCommands = true

        executor.execute {
            while (true) {
                try {
                    // Читаем длину сообщения (4 байта, BigEndian)
                    val sizeBytes = ByteArray(4)
                    var read = 0
                    while (read < 4) {
                        val n = inputStream?.read(sizeBytes, read, 4 - read) ?: -1
                        if (n < 0) throw Exception("Stream closed")
                        read += n
                    }

                    val msgSize = ((sizeBytes[0].toInt() and 0xFF) shl 24) or
                            ((sizeBytes[1].toInt() and 0xFF) shl 16) or
                            ((sizeBytes[2].toInt() and 0xFF) shl 8) or
                            (sizeBytes[3].toInt() and 0xFF)

                    // Читаем само сообщение
                    val msgData = ByteArray(msgSize)
                    read = 0
                    while (read < msgSize) {
                        val n = inputStream?.read(msgData, read, msgSize - read) ?: -1
                        if (n < 0) throw Exception("Stream closed")
                        read += n
                    }

                    // Парсим AgentCMD
                    val cmd = AgentCMD.parseFrom(msgData)
                    handleCommand(cmd)

                } catch (e: Exception) {
                    Log.e(TAG, "Command reading error: ${e.message}")
                    isReadingCommands = false
                    connectToAgent() // Переподключаемся
                    break
                }
            }
        }
    }


    private fun handleCommand(cmd: AgentCMD) {
        Log.d(TAG, "Received command: ${cmd.type} target=${cmd.targetId} payload=${cmd.payload}")

        when (cmd.type) {
            AgentCMD.CMDType.CLICK -> {
                handleClick(cmd)
            }
            AgentCMD.CMDType.TYPE_TEXT -> {
                handleTypeText(cmd)
            }
            AgentCMD.CMDType.GET_DUMP -> {
                sendDumpNow()
            }
            null -> {
                Log.e(TAG, "Unknown command type")
            }
            else -> {
                Log.e(TAG, "Unknown command type: ${cmd.type}")
            }
        }
    }

    private fun handleClick(cmd: AgentCMD) {
        // payload формат: "x,y" или "node_id"
        val parts = cmd.payload.split(",")

        if (parts.size == 2) {
            // Клик по координатам
            val x = parts[0].toIntOrNull()
            val y = parts[1].toIntOrNull()
            if (x != null && y != null) {
                clickAtCoordinates(x, y)
            }
        } else {
            // Клик по ID ноды (из UI-дерева)
            val nodeId = cmd.payload.toIntOrNull()
            if (nodeId != null) {
                clickByNodeId(nodeId)
            }
        }
    }

    private fun clickAtCoordinates(x: Int, y: Int) {
        mainHandler.post {
            val node = findNodeAt(x, y)
            if (node != null) {
                performClick(node)
                node.recycle()
            } else {
                Log.e(TAG, "No clickable node found at ($x, $y)")
                // Fallback: пробуем через dispatchGesture
                performGestureClick(x.toFloat(), y.toFloat())
            }
        }
    }

    private fun clickByNodeId(nodeId: Int) {
        mainHandler.post {
            val root = rootInActiveWindow
            if (root == null) {
                Log.e(TAG, "No active window")
                return@post
            }

            val node = findNodeById(root, nodeId)
            if (node != null) {
                performClick(node)
                node.recycle()
            } else {
                Log.e(TAG, "Node with id $nodeId not found")
            }
            root.recycle()
        }
    }

    private fun performClick(node: AccessibilityNodeInfo) {
        if (!node.isClickable) {
            Log.w(TAG, "Node is not clickable, trying to find parent")
            val clickableParent = findClickableParent(node)
            if (clickableParent != null) {
                val success = clickableParent.performAction(AccessibilityNodeInfo.ACTION_CLICK)
                Log.d(TAG, "Click on parent: $success")
                clickableParent.recycle()
                return
            }
        }

        val success = node.performAction(AccessibilityNodeInfo.ACTION_CLICK)
        Log.d(TAG, "Click performed: $success")
    }

    private fun handleTypeText(cmd: AgentCMD) {
        val text = cmd.payload
        mainHandler.post {
            // Если есть target_id — ищем поле ввода
            if (cmd.targetId.isNotEmpty()) {
                val nodeId = cmd.targetId.toIntOrNull()
                if (nodeId != null) {
                    val root = rootInActiveWindow ?: return@post
                    val node = findNodeById(root, nodeId)
                    if (node != null && node.isEditable) {
                        setTextToNode(node, text)
                        node.recycle()
                    }
                    root.recycle()
                    return@post
                }
            }

            // Иначе вставляем текст в фокус
            val focusedNode = findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            if (focusedNode != null && focusedNode.isEditable) {
                setTextToNode(focusedNode, text)
                focusedNode.recycle()
            } else {
                Log.e(TAG, "No editable field found")
            }
        }
    }

    private fun setTextToNode(node: AccessibilityNodeInfo, text: String) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.LOLLIPOP) {
            val arguments = android.os.Bundle()
            arguments.putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, text)
            val success = node.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, arguments)
            Log.d(TAG, "Set text: $success")
        } else {
            // Fallback для старых версий
            val success = node.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT)
            Log.d(TAG, "Set text (old API): $success")
        }
    }

    private fun performGestureClick(x: Float, y: Float) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.N) {
            Log.e(TAG, "GestureDescription requires API 24+")
            return
        }

        val path = Path().apply {
            moveTo(x, y)
        }

        val stroke = GestureDescription.StrokeDescription(path, 0, 50)
        val gestureBuilder = GestureDescription.Builder().apply {
            addStroke(stroke)
        }

        val gesture = gestureBuilder.build()
        val success = dispatchGesture(gesture, null, null)
        Log.d(TAG, "Gesture click: $success")
    }


    private fun findNodeAt(x: Int, y: Int): AccessibilityNodeInfo? {
        val root = rootInActiveWindow ?: return null
        val node = findNodeAtRecursive(root, x, y)
        root.recycle()
        return node
    }

    private fun findNodeAtRecursive(node: AccessibilityNodeInfo, x: Int, y: Int): AccessibilityNodeInfo? {
        val rect = android.graphics.Rect()
        node.getBoundsInScreen(rect)

        if (rect.contains(x, y)) {
            if (node.isClickable) {
                return AccessibilityNodeInfo.obtain(node)
            }
            for (i in 0 until node.childCount) {
                val child = node.getChild(i) ?: continue
                val found = findNodeAtRecursive(child, x, y)
                if (found != null) {
                    child.recycle()
                    return found
                }
                child.recycle()
            }
        }
        return null
    }

    private fun findNodeById(root: AccessibilityNodeInfo, targetId: Int): AccessibilityNodeInfo? {
        return findNodeByIdRecursive(root, targetId, AtomicInteger(0))
    }

    private fun findNodeByIdRecursive(
        node: AccessibilityNodeInfo,
        targetId: Int,
        counter: AtomicInteger
    ): AccessibilityNodeInfo? {
        val currentId = counter.incrementAndGet()

        if (currentId == targetId) {
            return AccessibilityNodeInfo.obtain(node)
        }

        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            val found = findNodeByIdRecursive(child, targetId, counter)
            if (found != null) {
                child.recycle()
                return found
            }
            child.recycle()
        }

        return null
    }

    private fun findClickableParent(node: AccessibilityNodeInfo): AccessibilityNodeInfo? {
        var parent = node.parent
        while (parent != null) {
            if (parent.isClickable) {
                return parent
            }
            val nextParent = parent.parent
            parent.recycle()
            parent = nextParent
        }
        return null
    }


    private fun sendDumpNow() {
        val rootNode = rootInActiveWindow ?: return
        executor.execute {
            val dump = captureScreen(rootNode)
            sendDump(dump)
            rootNode.recycle()
        }
    }

    private fun captureScreen(rootNode: AccessibilityNodeInfo): ScreenDump {
        nodeCounter.set(0)

        val dumpBuilder = ScreenDump.newBuilder()
            .setPackageName(rootNode.packageName?.toString() ?: "unknown")
            .setTimestamp(System.currentTimeMillis())
            .setWidth(1080)
            .setHeight(1920)

        flattenTree(rootNode, dumpBuilder, -1)
        return dumpBuilder.build()
    }

    private fun flattenTree(
        node: AccessibilityNodeInfo,
        dumpBuilder: ScreenDump.Builder,
        parentId: Int
    ) {
        val currentId = nodeCounter.incrementAndGet()

        val text = node.text?.toString() ?: ""
        val resId = node.viewIdResourceName ?: ""

        if (text.isNotEmpty() || resId.isNotEmpty() || node.isClickable || node.childCount > 0) {
            val bounds = android.graphics.Rect()
            node.getBoundsInScreen(bounds)

            val nodeProto = UiNode.newBuilder()
                .setId(currentId)
                .setParentId(parentId)
                .setText(text)
                .setResourceId(resId)
                .setClassName(node.className?.toString() ?: "")
                .setIsClickable(node.isClickable)
                .setBounds(
                    com.project.ratatoskr.Rect.newBuilder()
                        .setLeft(bounds.left)
                        .setRight(bounds.right)
                        .setTop(bounds.top)
                        .setBottom(bounds.bottom)
                        .build()
                )
                .build()

            dumpBuilder.addNodes(nodeProto)
        }

        for (i in 0 until node.childCount) {
            node.getChild(i)?.let { child ->
                flattenTree(child, dumpBuilder, currentId)
                child.recycle()
            }
        }
    }

    private fun sendDump(dump: ScreenDump) {
        try {
            outputStream?.let { stream ->
                val bytes = dump.toByteArray()
                val size = bytes.size
                stream.write(
                    byteArrayOf(
                        (size shr 24).toByte(),
                        (size shr 16).toByte(),
                        (size shr 8).toByte(),
                        size.toByte()
                    )
                )
                stream.write(bytes)
                stream.flush()
                Log.d(TAG, "Dump sent: ${dump.nodesCount} nodes")
            }
        } catch (e: Exception) {
            Log.e(TAG, "Sending error: ${e.message}")
            connectToAgent()
        }
    }


    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        if (event?.eventType == AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED ||
            event?.eventType == AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED
        ) {
            val rootNode = rootInActiveWindow ?: return
            executor.execute {
                val dump = captureScreen(rootNode)
                sendDump(dump)
                rootNode.recycle()
            }
        }
    }

    override fun onServiceConnected() {
        super.onServiceConnected()
        Log.d(TAG, "Ratatoskr service is running")

        // Настройка сервиса для лучшей производительности
        val info = AccessibilityServiceInfo().apply {
            eventTypes = AccessibilityEvent.TYPES_ALL_MASK
            feedbackType = AccessibilityServiceInfo.FEEDBACK_GENERIC
            flags = AccessibilityServiceInfo.FLAG_REPORT_VIEW_IDS or
                    AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS or
                    AccessibilityServiceInfo.FLAG_REQUEST_TOUCH_EXPLORATION_MODE
            notificationTimeout = 100
        }
        setServiceInfo(info)

        connectToAgent()
    }

    override fun onInterrupt() {
        Log.i(TAG, "Service interrupted")
    }

    override fun onDestroy() {
        super.onDestroy()
        executor.shutdown()
        try {
            socket?.close()
        } catch (e: Exception) {
            // ignore
        }
    }
}